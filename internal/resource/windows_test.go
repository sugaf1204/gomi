package resource

import "testing"

func TestValidateWindowsHostname(t *testing.T) {
	for _, name := range []string{"a", "win-node1", "ABCDEFGHIJKLMNO", "a1"} {
		if err := ValidateWindowsHostname(name); err != nil {
			t.Errorf("valid %s: %v", name, err)
		}
	}
	for _, name := range []string{"", "12345", "abcdefghijklmnop", "-windows", "windows-", "win_node", "win.node"} {
		if ValidateWindowsHostname(name) == nil {
			t.Errorf("invalid %s accepted", name)
		}
	}
}
