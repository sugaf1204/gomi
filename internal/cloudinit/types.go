package cloudinit

import (
	"time"
)

// DeliveryVMSeed restricts template data to the in-process VM seed renderer.
const DeliveryVMSeed = "vm-seed"

type CloudInitTemplate struct {
	Name string `json:"name"`

	DeliveryMode     string `json:"deliveryMode,omitempty"`
	UserData         string `json:"userData"`
	NetworkConfig    string `json:"networkConfig,omitempty"`
	MetadataTemplate string `json:"metadataTemplate,omitempty"`
	Description      string `json:"description,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
