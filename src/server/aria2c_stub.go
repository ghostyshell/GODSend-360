//go:build !darwin

package main

func darwinAria2cExtraCandidates() []string { return nil }

func ensureAria2cDarwinAtStartup() error { return nil }
