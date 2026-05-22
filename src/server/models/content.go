// content.go — domain types for DLC, Title Updates, and Xbox content packages.
package models

// ContentItem represents a single downloadable content package (DLC or TU).
type ContentItem struct {
	TitleID       string `json:"title_id"`
	ContentType   string `json:"content_type"`   // e.g. "00000002" (DLC), "000B0000" (TU)
	DisplayName   string `json:"display_name"`
	FileName      string `json:"file_name"`
	Size          int64  `json:"size,omitempty"`
	Version       int    `json:"version,omitempty"` // TU version number
	Source        string `json:"source"`            // "ia", "minerva", "xbox_cdn", "local"
	SourceURL     string `json:"source_url,omitempty"`
	Installed     bool   `json:"installed"`
	Active        bool   `json:"active"`            // for TUs: currently active
	OfferID       string `json:"offer_id,omitempty"`
}

// ContentManifest is what gets returned by discovery endpoints.
type ContentManifest struct {
	TitleID   string        `json:"title_id"`
	GameName  string        `json:"game_name"`
	DLCs      []ContentItem `json:"dlcs"`
	TitleUpdates []ContentItem `json:"title_updates"`
}

// InstalledContentReport is returned after scanning the Xbox Content directory.
type InstalledContentReport struct {
	TitleID      string        `json:"title_id"`
	DLCs         []ContentItem `json:"dlcs"`
	TitleUpdates []ContentItem `json:"title_updates"`
}

// ContentQueueRequest is sent by the client to queue a content download.
type ContentQueueRequest struct {
	GameName    string `json:"game_name"`
	TitleID     string `json:"title_id"`
	ContentType string `json:"content_type"`
	DisplayName string `json:"display_name"`
	FileName    string `json:"file_name,omitempty"`
	Source      string `json:"source"`
	SourceURL   string `json:"source_url,omitempty"`
	Drive       string `json:"drive,omitempty"`
	XboxIP      string `json:"xbox_ip,omitempty"`
}
