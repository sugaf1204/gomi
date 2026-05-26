package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"errors"
	"fmt"
	"strings"
	"time"
)

func updateZone(req *dnsv2.Msg) (string, bool) {
	if len(req.Question) != 1 {
		return "", false
	}
	zone, ok := canonicalFQDN(req.Question[0].Header().Name)
	if !ok {
		return "", false
	}
	return zone, true
}

func nameInZone(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

func dynamicTypeName(rrType uint16) string {
	switch rrType {
	case dnsv2.TypeA:
		return "A"
	case dnsv2.TypeTXT:
		return "TXT"
	case dnsv2.TypeCNAME:
		return "CNAME"
	default:
		return fmt.Sprintf("TYPE%d", rrType)
	}
}

func dynamicTypeFromName(name string) (uint16, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "A":
		return dnsv2.TypeA, true
	case "TXT":
		return dnsv2.TypeTXT, true
	case "CNAME":
		return dnsv2.TypeCNAME, true
	default:
		return 0, false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func updateErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, errDynamicNotZone):
		return dnsv2.RcodeNotZone
	case errors.Is(err, errDynamicUnsupported):
		return dnsv2.RcodeNotImplemented
	default:
		return dnsv2.RcodeFormatError
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

var (
	errDynamicFormat        = errors.New("dynamic dns update format error")
	errDynamicNotZone       = errors.New("dynamic dns update outside zone")
	errDynamicUnsupported   = errors.New("dynamic dns record type unsupported")
	errDynamicCNAMEConflict = errors.New("dynamic dns cname conflicts with existing records")
)
