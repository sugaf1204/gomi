package pxehttp

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"os"
	"path"
	"strings"
)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func (h *Handler) resolvePXEBaseURL(c echo.Context) string {
	if strings.TrimSpace(h.pxeHTTPBaseURL) != "" {
		return strings.TrimRight(h.pxeHTTPBaseURL, "/")
	}
	hostStr := strings.TrimSpace(c.Request().Host)
	if hostStr == "" {
		hostStr = "127.0.0.1:5392"
	}
	return "http://" + hostStr + "/pxe"
}

func sanitizePXEPath(raw string) (string, error) {
	p := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
		return "", fmt.Errorf("invalid path")
	}
	return p, nil
}

func envBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
