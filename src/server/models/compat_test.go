package models

import "testing"

// GTA V's Disc 1 "Install" disc has no default.xex to read a TitleID from
// (see GODSend-360 issue #4), so the name-based fallback is the only way
// Content install can resolve where to put the extracted files on the Xbox.
func TestGuessTitleIDFromMultiDiscName_GTAV(t *testing.T) {
	cases := []struct {
		name string
		want uint32
	}{
		{"Grand Theft Auto V (World) (Disc 1) (Install)", 0x545408A7},
		{"GTA V (USA) (Disc1)", 0x545408A7},
		{"GTA V [DVD1]", 0x545408A7},
		{"Grand Theft Auto V (World) (Disc 2) (Play)", 0}, // Disc 2 has its own xex
		// Must NOT collide with unrelated GTA titles that share the "V" prefix.
		{"Grand Theft Auto Vice City (USA) (Disc 1)", 0},
		{"Grand Theft Auto Vice City & San Andreas Double Pack (Disc 1)", 0},
	}
	for _, c := range cases {
		if got := GuessTitleIDFromMultiDiscName(c.name); got != c.want {
			t.Errorf("GuessTitleIDFromMultiDiscName(%q) = %08X, want %08X", c.name, got, c.want)
		}
	}
}

func TestIsNoExecutableInstallDiscName(t *testing.T) {
	if !IsNoExecutableInstallDiscName("Grand Theft Auto V (World) (Disc 1) (Install)") {
		t.Error("expected GTA V Disc 1 to match")
	}
	if IsNoExecutableInstallDiscName("Grand Theft Auto Vice City (USA) (Disc 1)") {
		t.Error("Vice City must not match the GTA V pattern")
	}
	if IsNoExecutableInstallDiscName("Grand Theft Auto V (World) (Disc 2) (Play)") {
		t.Error("GTA V Disc 2 has its own executable and must not match")
	}
}

// DiscCompat's disc<=1 shortcut only applies to games whose Disc 1 boots
// normally. GTA V's real Disc 2 ("Play" disc) needs an explicit table row
// or it silently falls back to the generic "content" default, which would
// tell the user to install the bootable disc as Content instead of GOD.
func TestDiscCompat_GTAVDisc2IsGOD(t *testing.T) {
	rec := DiscCompat(0x545408A7, 2)
	if rec.InstallType != "god" {
		t.Errorf("DiscCompat(GTA V, disc 2) = %q, want %q", rec.InstallType, "god")
	}
}
