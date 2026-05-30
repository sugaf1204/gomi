package main

// OSImage represents an OS image from the GOMI server API.
type OSImage struct {
	Name      string           `json:"name"`
	OSImageID string           `json:"osImageId"`
	Format    string           `json:"format"`
	Checksum  string           `json:"checksum,omitempty"`
	SizeBytes int64            `json:"sizeBytes,omitempty"`
	Ready     bool             `json:"ready"`
	Manifest  *OSImageManifest `json:"manifest,omitempty"`
}

type OSImageManifest struct {
	Root OSImageRootArtifact `json:"root"`
}

type OSImageRootArtifact struct {
	Format                string `json:"format"`
	Compression           string `json:"compression,omitempty"`
	Path                  string `json:"path"`
	SHA256                string `json:"sha256,omitempty"`
	UncompressedSizeBytes int64  `json:"uncompressedSizeBytes,omitempty"`
}
