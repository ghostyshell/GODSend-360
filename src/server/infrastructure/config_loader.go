package infrastructure

import (
	"os"
	"path/filepath"
)

// Config holds resolved filesystem paths and environment‑driven settings.
type Config struct {
	ToolsDir    string
	TransferDir string
	ReadyDir    string
	TempDir     string
	CacheDir    string
}

// LoadConfig computes default paths using the current working directory and
// the GODSEND_* environment variables. This is a thin wrapper around the
// existing configuration logic in main.go and will be wired into that code
// as part of the refactor.
func LoadConfig() (*Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	root := cwd
	tools := filepath.Join(root, "tools")

	return &Config{
		ToolsDir:    tools,
		TransferDir: filepath.Join(root, "Transfer"),
		ReadyDir:    filepath.Join(root, "Ready"),
		TempDir:     filepath.Join(root, "Temp"),
		CacheDir:    filepath.Join(root, "cache"),
	}, nil
}

