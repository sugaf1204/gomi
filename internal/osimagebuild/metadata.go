package osimagebuild

type ImageMetadata struct {
	Name      string   `json:"name"`
	OSFamily  string   `json:"osFamily"`
	OSVersion string   `json:"osVersion"`
	Arch      string   `json:"arch"`
	Variant   string   `json:"variant"`
	Artifact  string   `json:"artifact"`
	SHA256    string   `json:"sha256"`
	SizeBytes int64    `json:"sizeBytes"`
	Packages  []string `json:"packages,omitempty"`
}
