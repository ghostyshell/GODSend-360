package services

import "godsend/src/server/models"

// GameService coordinates game browsing, triggering downloads, and status checks.
type GameService interface {
	Browse(platform models.Platform, query string) ([]models.Game, error)
	Trigger(platform models.Platform, name string) error
	Status(name string) (models.JobStatus, string, error)
}

