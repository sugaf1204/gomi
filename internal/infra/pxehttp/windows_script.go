package pxehttp

import (
	_ "embed"
	"encoding/base64"
	"strings"
)

//go:embed windows/install.ps1
var windowsInstallScript string

//go:embed windows/complete.ps1
var windowsCompleteScript string

//go:embed windows/register.ps1
var windowsRegisterScript string

func renderWindowsInstall(config []byte) string {
	return strings.NewReplacer(
		"__GOMI_CONFIG__", base64.StdEncoding.EncodeToString(config),
		"__GOMI_REGISTER__", base64.StdEncoding.EncodeToString([]byte(windowsRegisterScript)),
		"__GOMI_COMPLETE__", base64.StdEncoding.EncodeToString([]byte(windowsCompleteScript)),
	).Replace(windowsInstallScript)
}
