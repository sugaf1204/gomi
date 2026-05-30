package api

import (
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
)

type OSImageResponse struct {
	Name        string              `json:"name"`
	OSImageID   string              `json:"osImageId"`
	OSFamily    string              `json:"osFamily"`
	OSVersion   string              `json:"osVersion"`
	Arch        string              `json:"arch"`
	Format      osimage.ImageFormat `json:"format"`
	Source      osimage.SourceType  `json:"source"`
	Variant     osimage.Variant     `json:"variant,omitempty"`
	URL         string              `json:"url,omitempty"`
	Checksum    string              `json:"checksum,omitempty"`
	SizeBytes   int64               `json:"sizeBytes,omitempty"`
	Description string              `json:"description,omitempty"`
	Manifest    *osimage.Manifest   `json:"manifest,omitempty"`
	Ready       bool                `json:"ready"`
	Error       string              `json:"error,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

func osImageResponses(items []osimage.OSImage) []OSImageResponse {
	out := make([]OSImageResponse, 0, len(items))
	for _, item := range items {
		out = append(out, osImageResponse(item))
	}
	return out
}

func osImageResponse(img osimage.OSImage) OSImageResponse {
	return OSImageResponse{
		Name:        resourceName("osImages", img.Name),
		OSImageID:   img.Name,
		OSFamily:    img.OSFamily,
		OSVersion:   img.OSVersion,
		Arch:        img.Arch,
		Format:      img.Format,
		Source:      img.Source,
		Variant:     img.Variant,
		URL:         img.URL,
		Checksum:    img.Checksum,
		SizeBytes:   img.SizeBytes,
		Description: img.Description,
		Manifest:    img.Manifest,
		Ready:       img.Ready,
		Error:       img.Error,
		CreatedAt:   img.CreatedAt,
		UpdatedAt:   img.UpdatedAt,
	}
}
