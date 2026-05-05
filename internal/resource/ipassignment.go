package resource

import (
	"fmt"
	"net"
	"strings"
)

type IPAssignmentMode string

const (
	IPAssignmentDHCP   IPAssignmentMode = "dhcp"
	IPAssignmentStatic IPAssignmentMode = "static"
)

// ValidateIPAssignment validates IP assignment mode and static IP consistency.
// Called from both machine and vm validation.
func ValidateIPAssignment(mode IPAssignmentMode, staticIP string) error {
	if mode != "" && mode != IPAssignmentDHCP && mode != IPAssignmentStatic {
		return fmt.Errorf("unsupported ipAssignment: %s", mode)
	}
	if mode == IPAssignmentStatic {
		if net.ParseIP(strings.TrimSpace(staticIP)) == nil {
			return fmt.Errorf("ip is required when ipAssignment is static")
		}
	}
	return nil
}
