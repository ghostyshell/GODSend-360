//go:build !darwin

package main

import (
	"godsend/app"
	"godsend/infrastructure/torrent"
)

func darwinAria2cExtraCandidates() []string { return nil }

func ensureAria2cDarwinAtStartup(_ *app.App, _ *torrent.Service) error { return nil }
