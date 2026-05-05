package api

import (
	gohttp "net/http"
	"os"
	"runtime"
	"time"

	"github.com/labstack/echo/v4"
)

func (s *Server) SystemInfo(c echo.Context) error {
	hostname, _ := os.Hostname()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	var uptime int64
	if !s.startTime.IsZero() {
		uptime = int64(time.Since(s.startTime).Seconds())
	}

	return c.JSON(gohttp.StatusOK, map[string]any{
		"hostname":     hostname,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"goVersion":    runtime.Version(),
		"cpuCount":     runtime.NumCPU(),
		"goroutines":   runtime.NumGoroutine(),
		"uptime":       uptime,
		"memoryUsedMB": mem.Alloc / 1024 / 1024,
	})
}
