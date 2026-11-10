// archive_password.go - password-aware archive extraction for the dlxbgame source.
package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode"
)

// ExtractArchiveWithPassword extracts a .zip/.7z/.rar archive to destDir.
// Password-protected 7z and RAR archives are supported; stdlib ZIP encryption
// is not, so .zip falls back to the unencrypted path.
func ExtractArchiveWithPassword(archivePath, destDir, password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ExtractArchive(archivePath, destDir)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".7z":
		r, err := sevenzip.OpenReaderWithPassword(archivePath, password)
		if err != nil {
			return fmt.Errorf("open 7z: %w", err)
		}
		defer r.Close()
		for _, f := range r.File {
			if err := extract7zEntry(f, destDir); err != nil {
				return err
			}
		}
		return nil
	case ".rar":
		r, err := rardecode.OpenReader(archivePath, password)
		if err != nil {
			return fmt.Errorf("open rar: %w", err)
		}
		defer r.Close()
		return drainRAR(r, destDir, "")
	default:
		return ExtractArchive(archivePath, destDir)
	}
}

// ExtractISOWithPassword extracts the first .iso inside a password-protected
// archive. Mirrors ExtractISO.
func ExtractISOWithPassword(archivePath, safeName, tempRoot, password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return ExtractISO(archivePath, safeName, tempRoot)
	}
	dest := filepath.Join(tempRoot, safeName+"_extracted")
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0755); err != nil {
		return "", err
	}
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".7z":
		r, err := sevenzip.OpenReaderWithPassword(archivePath, password)
		if err != nil {
			return "", fmt.Errorf("open 7z: %w", err)
		}
		defer r.Close()
		for _, f := range r.File {
			if strings.ToLower(filepath.Ext(f.Name)) != ".iso" {
				continue
			}
			if err := extract7zEntry(f, dest); err != nil {
				return "", err
			}
		}
	case ".rar":
		r, err := rardecode.OpenReader(archivePath, password)
		if err != nil {
			return "", fmt.Errorf("open rar: %w", err)
		}
		defer r.Close()
		if err := drainRAR(r, dest, ".iso"); err != nil {
			return "", err
		}
	default:
		return ExtractISO(archivePath, safeName, tempRoot)
	}
	iso := findFirstFileByExt(dest, ".iso")
	if iso == "" {
		return "", fmt.Errorf("no .iso found in archive")
	}
	return iso, nil
}
