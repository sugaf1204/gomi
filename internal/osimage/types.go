package osimage

import (
	"time"
)

type ImageFormat string

const (
	FormatQCOW2    ImageFormat = "qcow2"
	FormatRAW      ImageFormat = "raw"
	FormatISO      ImageFormat = "iso"
	FormatSquashFS ImageFormat = "squashfs"
)

type Variant string

const (
	VariantCloud     Variant = "cloud"
	VariantBareMetal Variant = "baremetal"
	VariantServer    Variant = "server"
	VariantDesktop   Variant = "desktop"
)

type SourceType string

const (
	SourceUpload SourceType = "upload"
	SourceURL    SourceType = "url"
)

type OSImage struct {
	Name string `json:"name"`

	// Spec fields
	OSFamily    string      `json:"osFamily"`
	OSVersion   string      `json:"osVersion"`
	Arch        string      `json:"arch"`
	Format      ImageFormat `json:"format"`
	Source      SourceType  `json:"source"`
	Variant     Variant     `json:"variant,omitempty"`
	URL         string      `json:"url,omitempty"`
	Checksum    string      `json:"checksum,omitempty"`
	SizeBytes   int64       `json:"sizeBytes,omitempty"`
	Description string      `json:"description,omitempty"`
	Manifest    *Manifest   `json:"manifest,omitempty"`

	// Status fields
	Ready     bool   `json:"ready"`
	LocalPath string `json:"localPath,omitempty"`
	Error     string `json:"error,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Manifest struct {
	SchemaVersion string        `json:"schemaVersion"`
	Name          string        `json:"name,omitempty"`
	OSFamily      string        `json:"osFamily,omitempty"`
	OSVersion     string        `json:"osVersion,omitempty"`
	Arch          string        `json:"arch,omitempty"`
	BootModes     []string      `json:"bootModes,omitempty"`
	Root          RootArtifact  `json:"root"`
	TargetKernel  TargetKernel  `json:"targetKernel"`
	Bundles       []Bundle      `json:"bundles,omitempty"`
	Build         BuildMetadata `json:"build,omitempty"`
}

type RootArtifact struct {
	Format                ImageFormat `json:"format"`
	Compression           string      `json:"compression,omitempty"`
	Path                  string      `json:"path"`
	SHA256                string      `json:"sha256,omitempty"`
	UncompressedSizeBytes int64       `json:"uncompressedSizeBytes,omitempty"`
	PartitionTable        string      `json:"partitionTable,omitempty"`
	RootPartition         Partition   `json:"rootPartition,omitempty"`
	BootPartition         *Partition  `json:"bootPartition,omitempty"`
	EFIPartition          *Partition  `json:"efiPartition,omitempty"`
	BIOSBootPartition     *Partition  `json:"biosBootPartition,omitempty"`
}

type Partition struct {
	Number     int    `json:"number,omitempty"`
	Filesystem string `json:"filesystem,omitempty"`
	PartUUID   string `json:"partuuid,omitempty"`
}

type TargetKernel struct {
	Version string `json:"version"`
	Flavor  string `json:"flavor,omitempty"`
	Source  string `json:"source,omitempty"`
}

type Bundle struct {
	ID                       string   `json:"id"`
	Type                     string   `json:"type"`
	KernelVersion            string   `json:"kernelVersion,omitempty"`
	Path                     string   `json:"path"`
	SHA256                   string   `json:"sha256,omitempty"`
	ProvidesModules          []string `json:"providesModules,omitempty"`
	ProvidesFirmwarePrefixes []string `json:"providesFirmwarePrefixes,omitempty"`
}

type BuildMetadata struct {
	CreatedAt         string   `json:"createdAt,omitempty"`
	BuilderVersion    string   `json:"builderVersion,omitempty"`
	SourceImageURL    string   `json:"sourceImageURL,omitempty"`
	SourceImageSHA256 string   `json:"sourceImageSHA256,omitempty"`
	AptSuite          string   `json:"aptSuite,omitempty"`
	AptSnapshot       string   `json:"aptSnapshot,omitempty"`
	PackageManager    string   `json:"packageManager,omitempty"`
	ModulePackages    []string `json:"modulePackages,omitempty"`
	FirmwareDirs      []string `json:"firmwareDirs,omitempty"`
	PackageLocks      []string `json:"packageLocks,omitempty"`
}
