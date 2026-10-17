package libvirt

type xmlTPM struct {
	Model   string        `xml:"model,attr"`
	Backend xmlTPMBackend `xml:"backend"`
}

type xmlTPMBackend struct {
	Type    string `xml:"type,attr"`
	Version string `xml:"version,attr"`
}

type xmlFirmware struct {
	Features []xmlFirmwareFeature `xml:"feature"`
}
type xmlFirmwareFeature struct {
	Enabled string `xml:"enabled,attr"`
	Name    string `xml:"name,attr"`
}
