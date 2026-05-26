package pxehttp

const deployEventImageApplied = "image_applied"

const provisionArtifactImageApplied = "imageApplied"

const provisionArtifactImageAppliedAt = "imageAppliedAt"

const provisionArtifactFailureLogTail = "failureLogTail"

const maxProvisionFailureLogTailLen = 32 * 1024

const maxProvisionTimingEvents = 240

const rootFSBIOSBootPartitionSizeMB int64 = 1

const rootFSEFIPartitionSizeMB int64 = 512

const rootFSPartitionReserveMB int64 = 64

const rootFSMinimumRootPartitionSizeMB int64 = 4096

type curtinInstall struct {
	LogFile           string   `yaml:"log_file"`
	PostFiles         []string `yaml:"post_files,omitempty"`
	SaveInstallConfig string   `yaml:"save_install_config"`
	SaveInstallLog    string   `yaml:"save_install_log"`
}

type curtinReportingHook struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	Level    string `yaml:"level"`
}

type curtinReporting struct {
	Gomi curtinReportingHook `yaml:"gomi"`
}

type curtinBlockMeta struct {
	Devices []string `yaml:"devices"`
}

type curtinSource struct {
	Type string `yaml:"type"`
	URI  string `yaml:"uri"`
}

type curtinStorageConfig struct {
	Version int              `yaml:"version"`
	Config  []map[string]any `yaml:"config"`
}

type curtinGrub struct {
	InstallDevices []string `yaml:"install_devices"`
}

type curtinStorage struct {
	Storage curtinStorageConfig `yaml:"storage"`
	Grub    curtinGrub          `yaml:"grub"`
}

type curtinConfig struct {
	Install              curtinInstall           `yaml:"install"`
	Reporting            curtinReporting         `yaml:"reporting"`
	BlockMeta            curtinBlockMeta         `yaml:"block-meta"`
	Sources              map[string]curtinSource `yaml:"sources"`
	Storage              *curtinStorageConfig    `yaml:"storage,omitempty"`
	Grub                 *curtinGrub             `yaml:"grub,omitempty"`
	Stages               []string                `yaml:"stages"`
	PartitioningCommands map[string][]string     `yaml:"partitioning_commands,omitempty"`
	LateCommands         map[string][]string     `yaml:"late_commands"`
}
