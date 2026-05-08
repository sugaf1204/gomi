package osimagebuild

type ImageMetadata struct {
	Name      string   `json:"name"`
	OSFamily  string   `json:"osFamily"`
	OSVersion string   `json:"osVersion"`
	Arch      string   `json:"arch"`
	Variant   string   `json:"variant,omitempty"`
	Format    string   `json:"format"`
	Artifact  string   `json:"artifact"`
	RootPath  string   `json:"rootPath"`
	SHA256    string   `json:"sha256"`
	SizeBytes int64    `json:"sizeBytes"`
	Packages  []string `json:"packages,omitempty"`
}
