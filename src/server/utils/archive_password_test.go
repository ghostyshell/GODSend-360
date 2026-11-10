package utils

import "testing"

func TestSafeArchivePath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"game.iso", "sub/game.rar", "a/b/c.bin", "./game.7z"} {
		if _, err := safeArchivePath(dir, name); err != nil {
			t.Errorf("safeArchivePath(%q) = %v, want ok", name, err)
		}
	}
	for _, name := range []string{"../evil", "..", "../../etc/passwd", "/abs/path", "a/../../evil"} {
		if _, err := safeArchivePath(dir, name); err == nil {
			t.Errorf("safeArchivePath(%q) = nil, want error", name)
		}
	}
}
