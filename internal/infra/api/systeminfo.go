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

	return c.JSON(gohttp.StatusOK, struct {
		Hostname     string `json:"hostname"`
		OS           string `json:"os"`
		Arch         string `json:"arch"`
		GoVersion    string `json:"goVersion"`
		CPUCount     int    `json:"cpuCount"`
		Goroutines   int    `json:"goroutines"`
		Uptime       int64  `json:"uptime"`
		MemoryUsedMB uint64 `json:"memoryUsedMB"`
	}{
		Hostname:     hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		GoVersion:    runtime.Version(),
		CPUCount:     runtime.NumCPU(),
		Goroutines:   runtime.NumGoroutine(),
		Uptime:       uptime,
		MemoryUsedMB: mem.Alloc / 1024 / 1024,
	})
}
