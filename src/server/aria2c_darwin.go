//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"godsend/app"
	"godsend/infrastructure/torrent"
)

func darwinAria2cExtraCandidates() []string {
	return []string{
		"/opt/homebrew/bin/aria2c",
		"/usr/local/bin/aria2c",
	}
}

// ensureAria2cDarwinAtStartup prepends Homebrew bin dirs to PATH, then ensures a
// working aria2c via Homebrew (installing Homebrew and aria2 non-interactively if needed).
func ensureAria2cDarwinAtStartup(a *app.App, t *torrent.Service) error {
	if strings.TrimSpace(os.Getenv("GODSEND_SKIP_ARIA2_BOOTSTRAP")) != "" {
		return nil
	}
	darwinPrependBrewToPath()
	if _, err := t.ProbeWorkingAria2c(); err == nil {
		return nil
	}

	brew := findBrewExecutable()
	if brew == "" {
		a.Logf("[INFO] Homebrew not found; running non-interactive installer (needs network)…")
		err := installHomebrewNonInteractive()
		if err != nil && darwinAllowGUIElevation() {
			a.Logf("[INFO] Non-interactive install failed (%v); showing macOS password dialog for sudo (Homebrew must not run as root)…", err)
			err = installHomebrewWithSudoAskpass()
		}
		if err != nil {
			return fmt.Errorf("could not install Homebrew: %w (install manually from https://brew.sh or set GODSEND_SKIP_ARIA2_BOOTSTRAP=1)", err)
		}
		darwinPrependBrewToPath()
		brew = findBrewExecutable()
		if brew == "" {
			return fmt.Errorf("Homebrew install finished but brew not found under /opt/homebrew/bin or /usr/local/bin")
		}
	}

	a.Logf("[INFO] Installing aria2 via Homebrew (%s)…", brew)
	if err := runBrewInstallAria2(brew); err != nil {
		return fmt.Errorf("brew install aria2: %w", err)
	}
	darwinPrependBrewToPath()
	if _, err := t.ProbeWorkingAria2c(); err != nil {
		return fmt.Errorf("aria2c still unavailable after brew install: %w", err)
	}
	a.Logf("[INFO] aria2c is ready for Minerva torrent downloads")
	return nil
}

func darwinPrependBrewToPath() {
	prefixes := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	path := os.Getenv("PATH")
	parts := strings.Split(path, string(os.PathListSeparator))
	have := make(map[string]bool, len(parts))
	for _, p := range parts {
		have[p] = true
	}
	var add []string
	for _, p := range prefixes {
		if !have[p] {
			add = append(add, p)
			have[p] = true
		}
	}
	if len(add) == 0 {
		return
	}
	os.Setenv("PATH", strings.Join(append(add, parts...), string(os.PathListSeparator)))
}

func findBrewExecutable() string {
	var candidates []string
	if runtime.GOARCH == "arm64" {
		candidates = []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}
	} else {
		candidates = []string{"/usr/local/bin/brew", "/opt/homebrew/bin/brew"}
	}
	for _, p := range candidates {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		return p
	}
	return ""
}

func darwinAllowGUIElevation() bool {
	if strings.TrimSpace(os.Getenv("GODSEND_NO_GUI_ELEVATION")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true") {
		return false
	}
	return true
}

func installHomebrewNonInteractive() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	script := "NONINTERACTIVE=1 CI=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

// installHomebrewWithSudoAskpass runs the official installer as the current user and sets
// SUDO_ASKPASS to a script that uses osascript's password dialog.
func installHomebrewWithSudoAskpass() error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	askpass, err := os.CreateTemp("", "godsend-askpass-*.sh")
	if err != nil {
		return err
	}
	apPath := askpass.Name()
	const askpassBody = `#!/bin/sh
exec osascript \
  -e 'display dialog "GODsend needs your administrator password to install Homebrew and aria2 for Minerva downloads." default answer "" with hidden answer with title "GODsend" buttons {"Cancel", "OK"} default button "OK"' \
  -e 'text returned of result'
`
	if _, werr := askpass.WriteString(askpassBody); werr != nil {
		_ = os.Remove(apPath)
		return werr
	}
	if cerr := askpass.Close(); cerr != nil {
		_ = os.Remove(apPath)
		return cerr
	}
	if err := os.Chmod(apPath, 0700); err != nil {
		_ = os.Remove(apPath)
		return err
	}
	defer func() { _ = os.Remove(apPath) }()

	script := `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"SUDO_ASKPASS="+apPath,
		"NONINTERACTIVE=1",
		"CI=1",
		"HOMEBREW_NO_ANALYTICS=1",
	)
	return cmd.Run()
}

func runBrewInstallAria2(brew string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, brew, "install", "aria2")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"NONINTERACTIVE=1",
		"CI=1",
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ANALYTICS=1",
	)
	return cmd.Run()
}
