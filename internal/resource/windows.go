package resource

import (
	"fmt"
	"regexp"
)

var windowsHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,13}[A-Za-z0-9])?$`)
var windowsNumericHostnamePattern = regexp.MustCompile(`^[0-9]+$`)

func ValidateWindowsHostname(name string) error {
	if !windowsHostnamePattern.MatchString(name) || windowsNumericHostnamePattern.MatchString(name) {
		return fmt.Errorf("Windows computer name must be 1-15 letters, digits, or hyphens, start and end with a letter or digit, and not be all digits")
	}
	return nil
}
