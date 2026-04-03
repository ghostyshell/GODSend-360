package models

// Core domain models and interfaces for the GODsend backend.

type Platform string

const (
	PlatformXbox360 Platform = "xbox360"
	PlatformXbox    Platform = "xbox"
	PlatformXBLA    Platform = "xbla"
	PlatformDigital Platform = "digital"
	PlatformDLC     Platform = "dlc"
	PlatformXBLIG   Platform = "xblig"
	PlatformLocal   Platform = "local"
)

type JobStatus string

const (
	JobStatusIdle       JobStatus = "Idle"
	JobStatusProcessing JobStatus = "Processing"
	JobStatusReady      JobStatus = "Ready"
	JobStatusError      JobStatus = "Error"
)

// Game describes a logical title in the catalog.
type Game struct {
	Name     string
	Platform Platform
}

// GameRepository exposes read access to available games per platform.
type GameRepository interface {
	Browse(platform Platform, query string) ([]Game, error)
}

// QueueRepository exposes persistence for long‑running jobs.
type QueueRepository interface {
	Enqueue(game Game) error
	List() ([]Game, error)
	Remove(name string) error
}

