package cloudinit

import (
	"time"
)

type CloudInitTemplate struct {
	Name string `json:"name"`

	UserData         string `json:"userData"`
	NetworkConfig    string `json:"networkConfig,omitempty"`
	MetadataTemplate string `json:"metadataTemplate,omitempty"`
	Description      string `json:"description,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
