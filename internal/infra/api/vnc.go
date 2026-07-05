package api

import (
	"crypto/rand"
	"encoding/hex"
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

const vmConsoleSessionTTL = 2 * time.Minute

type vmConsoleSession struct {
	VMName    string
	ExpiresAt time.Time
}

type vmConsoleSessionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Server) VNCProxy(c echo.Context) error {
	name := c.Param("name")

	m, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("machine not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if m.IP == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("machine has no IP address"))
	}

	vncAddr := fmt.Sprintf("%s:5900", m.IP)
	proxyVNC(c, vncAddr)
	return nil
}

func (s *Server) CreateVMConsoleSession(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("vm not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if v.Phase != vm.PhaseRunning && v.Phase != vm.PhaseProvisioning {
		return c.JSON(gohttp.StatusBadRequest, jsonError("vm is not running"))
	}

	token, err := newConsoleToken()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to issue console token"))
	}
	expiresAt := time.Now().UTC().Add(vmConsoleSessionTTL)
	s.storeVMConsoleSession(token, vmConsoleSession{
		VMName:    v.Name,
		ExpiresAt: expiresAt,
	})
	return c.JSON(gohttp.StatusCreated, vmConsoleSessionResponse{Token: token, ExpiresAt: expiresAt})
}

func (s *Server) VMVNCProxy(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	if !s.validateVMConsoleSession(name, c.QueryParam("console_token")) {
		return c.JSON(gohttp.StatusUnauthorized, jsonError("invalid console token"))
	}

	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("vm not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if v.Phase != "Running" && v.Phase != "Provisioning" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("vm is not running"))
	}

	// Resolve the hypervisor to get libvirt connection info.
	hv, err := s.hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError(fmt.Sprintf("resolve hypervisor: %v", err)))
	}

	cfg := vm.BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError(fmt.Sprintf("connect to hypervisor: %v", err)))
	}
	defer exec.Close()

	domainName := v.LibvirtDomain
	if domainName == "" {
		domainName = v.Name
	}

	graphics, err := exec.DomainGraphicsInfo(ctx, domainName)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError(fmt.Sprintf("get vnc info: %v", err)))
	}

	vncHost, err := resolveVNCProxyHost(graphics.Listen, hv.Connection.Host)
	if err != nil {
		return c.JSON(gohttp.StatusBadGateway, jsonErrorErr(err))
	}
	vncAddr := net.JoinHostPort(vncHost, fmt.Sprintf("%d", graphics.Port))
	proxyVNC(c, vncAddr)
	return nil
}

func newConsoleToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(tokenBytes), nil
}

func (s *Server) storeVMConsoleSession(token string, session vmConsoleSession) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	s.pruneExpiredConsoleSessionsLocked(time.Now().UTC())
	s.consoleSessions[token] = session
}

func (s *Server) validateVMConsoleSession(vmName, token string) bool {
	if token == "" {
		return false
	}
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	now := time.Now().UTC()
	s.pruneExpiredConsoleSessionsLocked(now)
	session, ok := s.consoleSessions[token]
	if !ok || session.VMName != vmName || now.After(session.ExpiresAt) {
		delete(s.consoleSessions, token)
		return false
	}
	return true
}

func (s *Server) pruneExpiredConsoleSessionsLocked(now time.Time) {
	for token, session := range s.consoleSessions {
		if now.After(session.ExpiresAt) {
			delete(s.consoleSessions, token)
		}
	}
}

func resolveVNCProxyHost(listen, hypervisorHost string) (string, error) {
	switch listen {
	case "", "0.0.0.0", "::":
		return hypervisorHost, nil
	case "127.0.0.1", "::1", "localhost":
		return "", fmt.Errorf("vnc console is bound to hypervisor loopback; refusing unsafe direct proxy")
	default:
		return listen, nil
	}
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
