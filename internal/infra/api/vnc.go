package api

import (
	"errors"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/websocket"

	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

func (s *Server) VNCProxy(c echo.Context) error {
	name := c.Param("name")

	m, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "machine not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if m.IP == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "machine has no IP address"})
	}

	vncAddr := fmt.Sprintf("%s:5900", m.IP)
	proxyVNC(c, vncAddr)
	return nil
}

func (s *Server) VMVNCProxy(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "vm not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if v.Phase != "Running" && v.Phase != "Provisioning" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "vm is not running"})
	}

	// Resolve the hypervisor to get libvirt connection info.
	hv, err := s.hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("resolve hypervisor: %v", err)})
	}

	cfg := vm.BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("connect to hypervisor: %v", err)})
	}
	defer exec.Close()

	domainName := v.LibvirtDomain
	if domainName == "" {
		domainName = v.Name
	}

	graphics, err := exec.DomainGraphicsInfo(ctx, domainName)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("get vnc info: %v", err)})
	}

	vncAddr := fmt.Sprintf("%s:%d", hv.Connection.Host, graphics.Port)
	proxyVNC(c, vncAddr)
	return nil
}

func proxyVNC(c echo.Context, vncAddr string) {
	handler := websocket.Handler(func(ws *websocket.Conn) {
		ws.PayloadType = websocket.BinaryFrame
		defer ws.Close()

		tcpConn, err := net.DialTimeout("tcp", vncAddr, 5*time.Second)
		if err != nil {
			return
		}
		defer tcpConn.Close()

		done := make(chan struct{}, 2)
		go func() {
			_, _ = io.Copy(tcpConn, ws)
			done <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(ws, tcpConn)
			done <- struct{}{}
		}()
		<-done
	})
	handler.ServeHTTP(c.Response(), c.Request())
}
