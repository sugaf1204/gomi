package libvirt

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type domainXMLDesc struct {
	Devices struct {
		Interfaces []struct {
			Target struct {
				Dev string `xml:"dev,attr"`
			} `xml:"target"`
			MAC struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
		} `xml:"interface"`
	} `xml:"devices"`
}

func parseDomainInterfacesFromXML(raw string) []InterfaceInfo {
	var desc domainXMLDesc
	if err := xml.Unmarshal([]byte(raw), &desc); err != nil {
		return nil
	}
	out := make([]InterfaceInfo, 0, len(desc.Devices.Interfaces))
	for _, iface := range desc.Devices.Interfaces {
		out = append(out, InterfaceInfo{
			Name: strings.TrimSpace(iface.Target.Dev),
			MAC:  strings.ToLower(strings.TrimSpace(iface.MAC.Address)),
		})
	}
	return out
}

// graphicsXMLDesc is used to parse VNC/SPICE graphics information from domain XML.
type graphicsXMLDesc struct {
	Devices struct {
		Graphics []struct {
			Type   string `xml:"type,attr"`
			Port   string `xml:"port,attr"`
			Listen string `xml:"listen,attr"`
		} `xml:"graphics"`
	} `xml:"devices"`
}

// parseDomainGraphicsFromXML extracts VNC graphics connection info from a domain XML description.
func parseDomainGraphicsFromXML(raw string) (*GraphicsInfo, error) {
	var desc graphicsXMLDesc
	if err := xml.Unmarshal([]byte(raw), &desc); err != nil {
		return nil, fmt.Errorf("parse domain xml: %w", err)
	}
	for _, g := range desc.Devices.Graphics {
		if g.Type != "vnc" {
			continue
		}
		port, err := strconv.Atoi(g.Port)
		if err != nil || port < 0 {
			return nil, fmt.Errorf("invalid vnc port: %s", g.Port)
		}
		return &GraphicsInfo{
			Type:   g.Type,
			Port:   port,
			Listen: g.Listen,
		}, nil
	}
	return nil, fmt.Errorf("no vnc graphics found in domain xml")
}

var (
	bootTagPattern = regexp.MustCompile(`<boot\s+dev=['"][^'"]+['"]\s*(?:/>|></boot>)`)
	osTagPattern   = regexp.MustCompile(`(?s)<os>.*?</os>`)
)

func rewriteDomainBootDeviceXML(rawXML string, bootDev string) (string, error) {
	switch bootDev {
	case "hd", "network":
	default:
		return "", fmt.Errorf("unsupported boot device: %s", bootDev)
	}

	osLoc := osTagPattern.FindStringIndex(rawXML)
	if len(osLoc) != 2 {
		return "", fmt.Errorf("os tag not found")
	}
	osSection := rawXML[osLoc[0]:osLoc[1]]
	newBootTag := fmt.Sprintf("<boot dev='%s'/>", bootDev)

	if bootTagPattern.MatchString(osSection) {
		updatedOS := bootTagPattern.ReplaceAllString(osSection, newBootTag)
		return rawXML[:osLoc[0]] + updatedOS + rawXML[osLoc[1]:], nil
	}

	closing := strings.LastIndex(osSection, "</os>")
	if closing < 0 {
		return "", fmt.Errorf("os closing tag not found")
	}
	updatedOS := osSection[:closing] + "    " + newBootTag + "\n  " + osSection[closing:]
	return rawXML[:osLoc[0]] + updatedOS + rawXML[osLoc[1]:], nil
}
