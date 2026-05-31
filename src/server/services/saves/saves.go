// saves.go — Xbox 360 save game management service.
package saves

import (
	"crypto/sha1"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"godsend/app"
	"godsend/infrastructure/ftp"
	"godsend/services"
)

// Service handles save-game discovery, listing, download, deletion, and resigning.
type Service struct {
	App    *app.App
	FTPMgr *ftp.Manager
}

// ProfileSaves describes save-game presence for a single Xbox profile.
type ProfileSaves struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	SaveCount   int    `json:"save_count"`
	LastModified string `json:"last_modified"`
}

// Known Xbox drives to scan for Content directories, in priority order.
var contentDrives = []string{"Usb0", "Usb1", "Hdd1", "HddX"}

// findContentDrive locates a drive that has a Content directory.
func (s *Service) findContentDrive(ip string) (string, error) {
	for _, drive := range contentDrives {
		path := fmt.Sprintf("/%s/Content", drive)
		if _, err := s.FTPMgr.List(ip, path); err == nil {
			s.App.Logf("SAVES: found Content on /%s/Content", drive)
			return drive, nil
		}
	}
	return "", fmt.Errorf("Content directory not found on any drive (tried %v)", contentDrives)
}

// resolveDrive returns the drive to use, auto-detecting if needed.
func (s *Service) resolveDrive(ip, drive string) (string, error) {
	clean := strings.TrimSuffix(drive, ":")
	if clean == "" || clean == "auto" {
		return s.findContentDrive(ip)
	}
	return clean, nil
}

// ListAllProfiles quickly lists every profile directory under Content
// without crawling individual titles. Used for copy-to target picker.
func (s *Service) ListAllProfiles(ip, drive string) ([]ProfileSaves, error) {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return nil, err
	}

	contentRoot := fmt.Sprintf("/%s/Content", cleanDrive)
	entries, err := s.FTPMgr.List(ip, contentRoot)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", contentRoot, err)
	}

	profiles := make([]ProfileSaves, 0)
	for _, entry := range entries {
		if entry.Type != "dir" {
			continue
		}
		if entry.Name == "0000000000000000" {
			continue
		}
		if len(entry.Name) != 16 || !isHexString(entry.Name) {
			continue
		}
		name := s.ResolveProfileName(ip, cleanDrive, entry.Name)
		profiles = append(profiles, ProfileSaves{ProfileID: entry.Name, ProfileName: name})
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles, nil
}

// DiscoverSaves scans Xbox Content directory for non-zero profile IDs
// that have save data (content type 00000001). If titleID is non-empty,
// only counts saves for that specific title (and fuzzy variants).
func (s *Service) DiscoverSaves(ip, drive, titleID string) ([]ProfileSaves, error) {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return nil, err
	}

	contentRoot := fmt.Sprintf("/%s/Content", cleanDrive)
	s.App.Logf("SAVES: discover on %s for title=%s", contentRoot, titleID)

	entries, err := s.FTPMgr.List(ip, contentRoot)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", contentRoot, err)
	}

	// Build list of title IDs to check: exact + fuzzy variants
	titleIDs := []string{}
	if titleID != "" {
		titleID = strings.ToUpper(titleID)
		titleIDs = append(titleIDs, titleID)
		// Also try common variants (last byte differs for content type)
		if len(titleID) == 8 {
			base := titleID[:6]
			for _, last := range []string{"FC", "FD", "FE", "FF", "F0", "F1", "F2"} {
				v := base + last
				if v != titleID {
					titleIDs = append(titleIDs, v)
				}
			}
		}
	}

	profiles := make([]ProfileSaves, 0)
	for _, entry := range entries {
		if entry.Type != "dir" {
			continue
		}
		if entry.Name == "0000000000000000" {
			continue
		}
		if len(entry.Name) != 16 || !isHexString(entry.Name) {
			continue
		}

		titleDir := fmt.Sprintf("%s/%s", contentRoot, entry.Name)
		titleEntries, err := s.FTPMgr.List(ip, titleDir)
		if err != nil {
			continue
		}

		saveCount := 0
		for _, te := range titleEntries {
			if te.Type != "dir" || len(te.Name) != 8 || !isHexString(te.Name) {
				continue
			}
			// Filter by title ID if provided
			if len(titleIDs) > 0 {
				matched := false
				for _, tid := range titleIDs {
					if strings.EqualFold(te.Name, tid) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			saveDir := fmt.Sprintf("%s/%s/00000001", titleDir, te.Name)
			saveEntries, err := s.FTPMgr.List(ip, saveDir)
			if err != nil {
				continue
			}
			if len(saveEntries) > 0 {
				saveCount += len(saveEntries)
			}
		}
		if saveCount > 0 {
			profiles = append(profiles, ProfileSaves{
				ProfileID:    entry.Name,
				ProfileName:  s.ResolveProfileName(ip, cleanDrive, entry.Name),
				SaveCount:    saveCount,
				LastModified: "",
			})
		}
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles, nil
}

// SaveEntry describes a single save file.
type SaveEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ListSaveFiles lists the files inside a specific save directory.
// Tries the exact title ID first, then fuzzy variants.
func (s *Service) ListSaveFiles(ip, drive, titleID, profileID string) ([]SaveEntry, error) {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return nil, err
	}

	// Build candidate title IDs: exact + fuzzy
	titleID = strings.ToUpper(titleID)
	candidates := []string{titleID}
	if len(titleID) == 8 {
		base := titleID[:6]
		for _, last := range []string{"FC", "FD", "FE", "FF", "F0", "F1", "F2"} {
			v := base + last
			if v != titleID {
				candidates = append(candidates, v)
			}
		}
	}

	profileID = strings.ToUpper(profileID)
	saves := make([]SaveEntry, 0)

	for _, tid := range candidates {
		savePath := fmt.Sprintf("/%s/Content/%s/%s/00000001", cleanDrive, profileID, tid)
		entries, err := s.FTPMgr.List(ip, savePath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type != "dir" {
				saves = append(saves, SaveEntry{Name: e.Name, Size: e.Size})
			}
		}
		if len(saves) > 0 {
			s.App.Logf("SAVES: found %d files at %s (title %s)", len(saves), savePath, tid)
			return saves, nil
		}
	}

	// No saves found — return empty slice, not error
	s.App.Logf("SAVES: no saves for %s/%s (tried %v)", profileID, titleID, candidates)
	return saves, nil
}

// resolveSaveTitleID finds the actual title ID directory that contains save files.
// Tries exact match first, then fuzzy variants (last 2 hex bytes differ).
func (s *Service) resolveSaveTitleID(ip, drive, profileID, titleID string) string {
	titleID = strings.ToUpper(titleID)
	profileID = strings.ToUpper(profileID)

	candidates := []string{titleID}
	if len(titleID) == 8 {
		base := titleID[:6]
		for _, last := range []string{"FC", "FD", "FE", "FF", "F0", "F1", "F2"} {
			v := base + last
			if v != titleID {
				candidates = append(candidates, v)
			}
		}
	}

	saveDir := fmt.Sprintf("/%s/Content/%s", drive, profileID)
	for _, tid := range candidates {
		testPath := fmt.Sprintf("%s/%s/00000001", saveDir, tid)
		entries, err := s.FTPMgr.List(ip, testPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type != "dir" {
				s.App.Logf("SAVES: resolved title %s -> %s", titleID, tid)
				return tid
			}
		}
	}
	return titleID // fall back to original
}

// DownloadSave downloads all save files from Xbox to a local backup folder.
// Layout: <localDir>/Saves/<gamertag> (<profileID>)/<gameName> - <titleID>/<files>
func (s *Service) DownloadSave(ip, drive, titleID, profileID, localDir, gameName string) error {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return err
	}

	actualTitleID := s.resolveSaveTitleID(ip, cleanDrive, profileID, titleID)

	remotePath := fmt.Sprintf("/%s/Content/%s/%s/00000001", cleanDrive,
		strings.ToUpper(profileID), actualTitleID)

	entries, err := s.FTPMgr.List(ip, remotePath)
	if err != nil {
		return fmt.Errorf("list save files for download: %w", err)
	}

	var fileNames []string
	for _, entry := range entries {
		if entry.Type != "dir" {
			fileNames = append(fileNames, entry.Name)
		}
	}
	if len(fileNames) == 0 {
		return fmt.Errorf("no save files found at %s", remotePath)
	}

	gameFolder := titleID
	if gameName != "" {
		gameFolder = gameName + " - " + titleID
	}
	profileFolder := profileFolderName(s.ResolveProfileName(ip, cleanDrive, profileID), profileID)
	localDirPath := filepath.Join(localDir, "Saves", profileFolder, gameFolder)
	if err := os.MkdirAll(localDirPath, 0755); err != nil {
		return fmt.Errorf("create local dir %s: %w", localDirPath, err)
	}

	for _, name := range fileNames {
		rp := filepath.ToSlash(filepath.Join(remotePath, name))
		lp := filepath.Join(localDirPath, name)
		s.App.Logf("SAVES: downloading %s -> %s", rp, lp)
		if err := s.FTPMgr.DownloadFile(ip, rp, lp); err != nil {
			return fmt.Errorf("download %s: %w", name, err)
		}
	}

	s.App.Logf("SAVES: downloaded %d files for %s/%s", len(fileNames), titleID, profileID)
	return nil
}

// DeleteSave deletes the entire save directory from Xbox.
func (s *Service) DeleteSave(ip, drive, titleID, profileID string) error {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return err
	}

	actualTitleID := s.resolveSaveTitleID(ip, cleanDrive, profileID, titleID)

	savePath := fmt.Sprintf("/%s/Content/%s/%s/00000001", cleanDrive,
		strings.ToUpper(profileID), actualTitleID)
	return s.FTPMgr.Delete(ip, savePath)
}

// ── Save resigning (STFS/CON format) ──────────────────────────────────────

// CONHeader holds the parsed header of a CON/STFS save file.
type CONHeader struct {
	Magic       [4]byte // "CON " or "PIRS" or "LIVE"
	ConsoleID   [5]byte // offset 0x06
	ProfileID   [8]byte // offset 0x371
	Raw         []byte  // full file bytes
	HeaderSize  uint32  // offset 0x340
}

// ParseCONHeader reads the CON/STFS header from a save file.
func ParseCONHeader(data []byte) (*CONHeader, error) {
	if len(data) < 0x400 {
		return nil, fmt.Errorf("file too small for CON header: %d bytes", len(data))
	}

	h := &CONHeader{Raw: make([]byte, len(data))}
	copy(h.Raw, data)

	copy(h.Magic[:], data[0x00:0x04])
	magic := string(h.Magic[:])
	if magic != "CON " && magic != "PIRS" && magic != "LIVE" {
		return nil, fmt.Errorf("not a CON file (magic: %q)", magic)
	}

	copy(h.ConsoleID[:], data[0x06:0x0B])
	h.HeaderSize = uint32(data[0x340])<<24 | uint32(data[0x341])<<16 |
		uint32(data[0x342])<<8 | uint32(data[0x343])
	copy(h.ProfileID[:], data[0x371:0x379])

	return h, nil
}

// UpdateProfileID changes the profile ID in the CON header and recomputes
// the SHA-1 header hash. Returns true if the file was modified.
func (h *CONHeader) UpdateProfileID(newProfileID uint64) {
	// Write new profile ID at offset 0x371 (big-endian)
	h.Raw[0x371] = byte(newProfileID >> 56)
	h.Raw[0x372] = byte(newProfileID >> 48)
	h.Raw[0x373] = byte(newProfileID >> 40)
	h.Raw[0x374] = byte(newProfileID >> 32)
	h.Raw[0x375] = byte(newProfileID >> 24)
	h.Raw[0x376] = byte(newProfileID >> 16)
	h.Raw[0x377] = byte(newProfileID >> 8)
	h.Raw[0x378] = byte(newProfileID)

	// Rehash: SHA-1 of bytes from 0x344 to h.HeaderSize
	headerEnd := h.HeaderSize
	if int(headerEnd) > len(h.Raw) {
		headerEnd = uint32(len(h.Raw))
	}
	hash := sha1.Sum(h.Raw[0x344:headerEnd])
	copy(h.Raw[0x32C:0x340], hash[:])
}

// ResignWithKeyvault re-signs the RSA signature at offset 0x1AC
// using the console's private key from the keyvault.
func (h *CONHeader) ResignWithKeyvault(kv *KeyVault) error {
	// SHA-1 hash of bytes from 0x00 to 0x1AB
	msgHash := sha1.Sum(h.Raw[0x00:0x1AC])

	// PKCS#1 v1.5 padding for SHA-1 with 1024-bit (0x80 byte) key
	padded := pkcs1v15PadSHA1(msgHash[:], 0x80)

	m := new(big.Int).SetBytes(padded)

	// CRT-based RSA signing: s = m^d mod n
	n := new(big.Int).Mul(kv.P, kv.Q)
	d := computePrivKey(kv)

	s := new(big.Int).Exp(m, d, n)
	sig := make([]byte, 0x80)
	sigBytes := s.Bytes()
	copy(sig[0x80-len(sigBytes):], sigBytes)
	copy(h.Raw[0x1AC:0x22C], sig)

	return nil
}

// KeyVault holds the console's RSA private key parameters (CRT form).
type KeyVault struct {
	P    *big.Int // 0x80 bytes
	Q    *big.Int // 0x80 bytes
	DP   *big.Int // 0x80 bytes
	DQ   *big.Int // 0x80 bytes
	QInv *big.Int // 0x80 bytes
}

// computePrivKey reconstructs the private exponent d from CRT parameters.
func computePrivKey(kv *KeyVault) *big.Int {
	// d = DP mod (P-1)  =>  d ≡ DP (mod P-1)
	// We compute n = P*Q, phi = (P-1)*(Q-1)
	// Then d = e^-1 mod phi where e=65537
	e := big.NewInt(65537)
	p1 := new(big.Int).Sub(kv.P, big.NewInt(1))
	q1 := new(big.Int).Sub(kv.Q, big.NewInt(1))
	phi := new(big.Int).Mul(p1, q1)
	d := new(big.Int).ModInverse(e, phi)
	return d
}

// TryFindKeyVaultOnConsole attempts to locate and download a keyvault
// from common paths on the Xbox via FTP.
func (s *Service) TryFindKeyVaultOnConsole(ip string) (*KeyVault, error) {
	paths := []string{
		"/Hdd1/kv.bin",
		"/Hdd1/keyvault.bin",
		"/Hdd1/cr.bin",
		"/HddX/kv.bin",
		"/Usb0/kv.bin",
	}

	for _, p := range paths {
		dir := filepath.Dir(p)
		base := filepath.Base(p)
		entries, err := s.FTPMgr.List(ip, dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.EqualFold(e.Name, base) && e.Type != "dir" {
				s.App.Logf("SAVES: found keyvault at %s", p)
				kv, err := s.downloadAndParseKV(ip, p)
				if err != nil {
					s.App.Logf("SAVES: failed to parse keyvault at %s: %v", p, err)
					continue
				}
				return kv, nil
			}
		}
	}
	return nil, fmt.Errorf("keyvault not found on console")
}

func (s *Service) downloadAndParseKV(ip, remotePath string) (*KeyVault, error) {
	tmpFile := filepath.Join(s.App.ToolsDir, "Temp", "godsend_kv_"+ipToFilename(ip)+".bin")
	if err := s.FTPMgr.DownloadFile(ip, remotePath, tmpFile); err != nil {
		return nil, err
	}
	return ParseKeyVaultFile(tmpFile)
}

func ipToFilename(ip string) string {
	return strings.ReplaceAll(ip, ".", "_")
}

// ParseKeyVaultFile parses a decrypted keyvault binary.
// Expected offsets in a retail decrypted keyvault (0x4000 bytes):
//
//	0x20C: P (0x80 bytes)
//	0x28C: Q (0x80 bytes)
//	0x30C: DP (0x80 bytes)
//	0x38C: DQ (0x80 bytes)
//	0x40C: QInv (0x80 bytes)
func ParseKeyVaultFile(path string) (*KeyVault, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try 0x4000-byte layout first, then 0x800-byte
	offsets := []struct{ p, q, dp, dq, qinv int }{
		{0x20C, 0x28C, 0x30C, 0x38C, 0x40C}, // standard decrypted kv
	}
	// Check for 16KB (0x4000) keyvault
	if len(data) >= 0x1000 {
		offsets = append(offsets, struct{ p, q, dp, dq, qinv int }{
			0x20C + 0x1000, 0x28C + 0x1000, 0x30C + 0x1000, 0x38C + 0x1000, 0x40C + 0x1000,
		})
	}

	for _, off := range offsets {
		if len(data) < off.qinv+0x80 {
			continue
		}
		kv := &KeyVault{
			P:    new(big.Int).SetBytes(data[off.p : off.p+0x80]),
			Q:    new(big.Int).SetBytes(data[off.q : off.q+0x80]),
			DP:   new(big.Int).SetBytes(data[off.dp : off.dp+0x80]),
			DQ:   new(big.Int).SetBytes(data[off.dq : off.dq+0x80]),
			QInv: new(big.Int).SetBytes(data[off.qinv : off.qinv+0x80]),
		}
		if kv.P.Sign() > 0 && kv.Q.Sign() > 0 {
			return kv, nil
		}
	}

	return nil, fmt.Errorf("could not find valid RSA parameters in keyvault (size=%d)", len(data))
}

// ResolveProfileName downloads the account STFS file for a profile
// and extracts the gamertag. Returns "" if extraction fails.
func (s *Service) ResolveProfileName(ip, drive, profileID string) string {
	accountPath := fmt.Sprintf("/%s/Content/%s/FFFE07D1/00010000/%s", drive,
		strings.ToUpper(profileID), strings.ToUpper(profileID))

	tmpFile := filepath.Join(s.App.ToolsDir, "Temp", "godsend_acc_"+profileID+".bin")
	if err := s.FTPMgr.DownloadFile(ip, accountPath, tmpFile); err != nil {
		s.App.Logf("SAVES: cannot download account for %s: %v", profileID, err)
		return ""
	}
	defer os.Remove(tmpFile)

	data, err := os.ReadFile(tmpFile)
	if err != nil || len(data) < 0x400 {
		return ""
	}

	// Preferred path: walk the STFS file table, decrypt the embedded Account
	// file, read the UTF-16BE gamertag. Matches Velocity / py360 behaviour.
	if gt := ExtractGamertagFromProfilePackage(data); gt != "" {
		s.App.Logf("SAVES: resolved profile %s -> %s (account decrypt)", profileID, gt)
		return gt
	}
	s.App.Logf("SAVES: account decrypt failed for %s, falling back to ASCII scan", profileID)

	// Fallback: scan data blocks for the first plausible ASCII string. Less
	// reliable but rescues malformed or non-standard profile packages.
	magic := string(data[0:4])
	if magic != "CON " && magic != "PIRS" && magic != "LIVE" {
		return ""
	}
	headerSize := uint32(data[0x340])<<24 | uint32(data[0x341])<<16 |
		uint32(data[0x342])<<8 | uint32(data[0x343])

	start := int(headerSize)
	if start < 0x400 || start >= len(data) {
		start = 0x400
	}
	end := start + 0x10000
	if end > len(data) {
		end = len(data)
	}

	best := ""
	run := make([]byte, 0, 32)
	for i := start; i < end; i++ {
		b := data[i]
		if b >= 0x20 && b < 0x7F {
			if len(run) < 32 {
				run = append(run, b)
			}
		} else {
			if len(run) >= 3 && len(run) <= 16 {
				s := string(run)
				if !isOnlyDigits(s) && !looksLikeHex(s) && !looksLikePath(s) {
					if best == "" || len(s) > len(best) {
						best = s
						break
					}
				}
			}
			run = run[:0]
		}
	}

	if best != "" {
		s.App.Logf("SAVES: resolved profile %s -> %s (ascii scan)", profileID, best)
	}
	return best
}

func isOnlyDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func looksLikeHex(s string) bool {
	hexChars := 0
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') {
			hexChars++
		}
	}
	return hexChars == len(s) && len(s) >= 8
}

func looksLikePath(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.Contains(s, ".")
}

// CopySaveToProfile copies a save from source profile to destination profile.
// If a keyvault is provided, the save will be resigned for the target profile.
// If no keyvault is provided, the raw file is copied directly (may not work
// for all games, but some accept same-console copies without resigning).
func (s *Service) CopySaveToProfile(ip, drive, titleID, srcProfile, dstProfile, localDir string, kv *KeyVault) (*CopyResult, error) {
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return nil, err
	}

	actualTitleID := s.resolveSaveTitleID(ip, cleanDrive, srcProfile, titleID)

	srcPath := fmt.Sprintf("/%s/Content/%s/%s/00000001", cleanDrive,
		strings.ToUpper(srcProfile), actualTitleID)

	entries, err := s.FTPMgr.List(ip, srcPath)
	if err != nil {
		return nil, fmt.Errorf("list source saves: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.Type != "dir" {
			files = append(files, e.Name)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no save files at %s", srcPath)
	}

	// Ensure target directory exists on Xbox
	dstDir := fmt.Sprintf("/%s/Content/%s/%s/00000001", cleanDrive,
		strings.ToUpper(dstProfile), actualTitleID)
	if err := s.FTPMgr.Mkdir(ip, dstDir); err != nil {
		s.App.Logf("SAVES: mkdir %s: %v (may already exist)", dstDir, err)
	}

	// Temp directory for processing
	tempDir := filepath.Join(localDir, "Saves", "_copy_"+titleID+"_"+srcProfile)
	os.MkdirAll(tempDir, 0755)

	result := &CopyResult{
		SourceProfile:    srcProfile,
		DestProfile:      dstProfile,
		FilesCopied:      len(files),
		Resigned:         kv != nil,
	}

	var newProfID uint64
	fmt.Sscanf(strings.ToUpper(dstProfile), "%016X", &newProfID)

	for _, name := range files {
		rp := filepath.ToSlash(filepath.Join(srcPath, name))
		lp := filepath.Join(tempDir, name)

		// Download
		if err := s.FTPMgr.DownloadFile(ip, rp, lp); err != nil {
			return nil, fmt.Errorf("download %s: %w", name, err)
		}

		if kv != nil {
			// Parse, update profile ID, rehash, and resign
			data, err := os.ReadFile(lp)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", name, err)
			}
			con, err := ParseCONHeader(data)
			if err != nil {
				// Not a CON file? Copy raw.
				s.App.Logf("SAVES: %s is not CON (%v) — copying raw", name, err)
			} else {
				oldPID := fmt.Sprintf("%016X", uint64(con.ProfileID[0])<<56|uint64(con.ProfileID[1])<<48|
					uint64(con.ProfileID[2])<<40|uint64(con.ProfileID[3])<<32|
					uint64(con.ProfileID[4])<<24|uint64(con.ProfileID[5])<<16|
					uint64(con.ProfileID[6])<<8|uint64(con.ProfileID[7]))
				s.App.Logf("SAVES: resigning %s from %s to %s", name, oldPID, dstProfile)
				con.UpdateProfileID(newProfID)
				if err := con.ResignWithKeyvault(kv); err != nil {
					return nil, fmt.Errorf("resign %s: %w", name, err)
				}
				if err := os.WriteFile(lp, con.Raw, 0644); err != nil {
					return nil, fmt.Errorf("write resigned %s: %w", name, err)
				}
			}
		}

		// Upload to destination
		dstPath := filepath.ToSlash(filepath.Join(dstDir, name))
		if err := s.FTPMgr.UploadSingleFile(ip, lp, dstPath); err != nil {
			return nil, fmt.Errorf("upload %s to destination: %w", name, err)
		}

		s.App.Logf("SAVES: copied %s -> %s", rp, dstPath)
	}

	os.RemoveAll(tempDir)
	return result, nil
}

// CopyResult describes the outcome of a copy+resign operation.
type CopyResult struct {
	SourceProfile string `json:"source_profile"`
	DestProfile   string `json:"dest_profile"`
	FilesCopied   int    `json:"files_copied"`
	Resigned      bool   `json:"resigned"`
}

// ── Bulk backup ───────────────────────────────────────────────────────

// BackupAllResult summarises a BackupAllProfiles run.
type BackupAllResult struct {
	ProfilesProcessed int      `json:"profiles_processed"`
	ProfilesBackedUp  int      `json:"profiles_backed_up"`
	SavesBackedUp     int      `json:"saves_backed_up"`
	FilesBackedUp     int      `json:"files_backed_up"`
	Errors            []string `json:"errors,omitempty"`
}

// BackupAllProfiles iterates every profile under /Content, copies the profile
// STFS package itself, then copies every save (content type 00000001) for
// every title that profile has saves for. Layout:
//
//	<localDir>/Saves/<gamertag> (<profileID>)/Profile/<profileID>
//	<localDir>/Saves/<gamertag> (<profileID>)/<gameName> - <titleID>/<files>
//
// Errors against individual profiles/titles are collected and returned in the
// result rather than aborting the whole backup.
func (s *Service) BackupAllProfiles(ip, drive, localDir string) (*BackupAllResult, error) {
	if localDir == "" {
		return nil, fmt.Errorf("save backup folder is not set")
	}
	cleanDrive, err := s.resolveDrive(ip, drive)
	if err != nil {
		return nil, err
	}

	contentRoot := fmt.Sprintf("/%s/Content", cleanDrive)
	entries, err := s.FTPMgr.List(ip, contentRoot)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", contentRoot, err)
	}

	res := &BackupAllResult{}
	for _, e := range entries {
		if e.Type != "dir" || len(e.Name) != 16 || !isHexString(e.Name) {
			continue
		}
		if e.Name == "0000000000000000" {
			continue
		}
		profileID := strings.ToUpper(e.Name)
		res.ProfilesProcessed++

		gamertag := s.ResolveProfileName(ip, cleanDrive, profileID)
		profileDir := filepath.Join(localDir, "Saves", profileFolderName(gamertag, profileID))

		// 1. Profile STFS package
		if err := s.backupProfilePackage(ip, cleanDrive, profileID, profileDir); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: profile package: %v", profileID, err))
		} else {
			res.ProfilesBackedUp++
			res.FilesBackedUp++
		}

		// 2. Per-title save files
		titleEntries, err := s.FTPMgr.List(ip, contentRoot+"/"+profileID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: list titles: %v", profileID, err))
			continue
		}
		for _, te := range titleEntries {
			if te.Type != "dir" || len(te.Name) != 8 || !isHexString(te.Name) {
				continue
			}
			titleID := strings.ToUpper(te.Name)
			if titleID == "FFFE07D1" { // profile asset folder, already handled above
				continue
			}
			n, err := s.backupTitleSaves(ip, cleanDrive, profileID, titleID, profileDir)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s/%s: %v", profileID, titleID, err))
				continue
			}
			if n > 0 {
				res.SavesBackedUp++
				res.FilesBackedUp += n
			}
		}
	}

	// Drop any per-profile directory that wound up empty (no Account file +
	// no saves) — leaving stray empty dirs is just clutter.
	if rootDir := filepath.Join(localDir, "Saves"); rootDir != "" {
		if children, err := os.ReadDir(rootDir); err == nil {
			for _, c := range children {
				if c.IsDir() {
					_ = os.Remove(filepath.Join(rootDir, c.Name())) // succeeds only if empty
				}
			}
		}
	}

	s.App.Logf("SAVES: backup-all done — %d profiles, %d saves, %d files (%d errors)",
		res.ProfilesProcessed, res.SavesBackedUp, res.FilesBackedUp, len(res.Errors))
	return res, nil
}

// backupProfilePackage downloads /Content/<profileID>/FFFE07D1/00010000/<profileID>
// to <profileDir>/Profile/<profileID>. On FTP failure (no account file), the
// empty placeholder file and its parent dir are removed.
func (s *Service) backupProfilePackage(ip, drive, profileID, profileDir string) error {
	src := fmt.Sprintf("/%s/Content/%s/FFFE07D1/00010000/%s", drive, profileID, profileID)
	dstDir := filepath.Join(profileDir, "Profile")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dst := filepath.Join(dstDir, profileID)
	if err := s.FTPMgr.DownloadFile(ip, src, dst); err != nil {
		os.Remove(dst)
		os.Remove(dstDir) // only succeeds if empty
		return err
	}
	return nil
}

// backupTitleSaves downloads every file under Content/<profile>/<title>/00000001/
// to <profileDir>/<gameName> - <titleID>/. Returns count of files downloaded.
func (s *Service) backupTitleSaves(ip, drive, profileID, titleID, profileDir string) (int, error) {
	remoteSaveDir := fmt.Sprintf("/%s/Content/%s/%s/00000001", drive, profileID, titleID)
	entries, err := s.FTPMgr.List(ip, remoteSaveDir)
	if err != nil {
		return 0, nil // no saves for this title; not an error
	}

	var fileNames []string
	for _, e := range entries {
		if e.Type != "dir" {
			fileNames = append(fileNames, e.Name)
		}
	}
	if len(fileNames) == 0 {
		return 0, nil
	}

	gameName := services.LookupTitleName(titleID)
	gameFolder := titleID
	if gameName != "" {
		gameFolder = sanitizePathComponent(gameName) + " - " + titleID
	}
	dstDir := filepath.Join(profileDir, gameFolder)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return 0, fmt.Errorf("create %s: %w", dstDir, err)
	}

	n := 0
	for _, name := range fileNames {
		src := filepath.ToSlash(filepath.Join(remoteSaveDir, name))
		dst := filepath.Join(dstDir, name)
		if err := s.FTPMgr.DownloadFile(ip, src, dst); err != nil {
			s.App.Logf("SAVES: backup-all: skip %s: %v", src, err)
			continue
		}
		n++
	}
	return n, nil
}

// profileFolderName builds the per-profile folder name: "Gamertag (XUID)" if
// a gamertag is known, otherwise just the XUID.
func profileFolderName(gamertag, profileID string) string {
	if gamertag = sanitizePathComponent(gamertag); gamertag != "" {
		return gamertag + " (" + profileID + ")"
	}
	return profileID
}

// sanitizePathComponent strips characters that are illegal or awkward in
// filesystem paths (Windows-style invalid chars + leading/trailing space/dot).
func sanitizePathComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		`"`, "_", "<", "_", ">", "_", "|", "_",
	)
	s = replacer.Replace(s)
	return strings.Trim(s, " .")
}

// ── PKCS#1 v1.5 padding for SHA-1 ─────────────────────────────────────

func pkcs1v15PadSHA1(hash []byte, keyLen int) []byte {
	// DER-encoded DigestInfo for SHA-1:
	// 30 21 30 09 06 05 2B 0E 03 02 1A 05 00 04 14 <20-byte-hash>
	oid := []byte{0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2B, 0x0E,
		0x03, 0x02, 0x1A, 0x05, 0x00, 0x04, 0x14}
	tLen := len(oid) + len(hash)
	padLen := keyLen - tLen - 3

	padded := make([]byte, keyLen)
	padded[1] = 0x01
	for i := 0; i < padLen; i++ {
		padded[2+i] = 0xFF
	}
	padded[2+padLen] = 0x00
	copy(padded[2+padLen+1:], oid)
	copy(padded[2+padLen+1+len(oid):], hash)
	return padded
}

// ── Helpers ────────────────────────────────────────────────────────────────

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// Ensure io import is used
var _ = io.Discard
