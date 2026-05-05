package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type machinePowerEventRequest struct {
	RequestID     string `json:"requestID"`
	Stage         string `json:"stage"`
	Message       string `json:"message,omitempty"`
	DaemonVersion string `json:"daemonVersion,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

func (s *Server) ReportMachinePowerEvent(c echo.Context) error {
	name := c.Param("name")
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	m, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if m.Power.Type != power.PowerTypeWoL || m.Power.WoL == nil || strings.TrimSpace(m.Power.WoL.HMACSecret) == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "machine does not have WoL callback signing configured"})
	}
	if !verifyPowerEventSignature(raw, m.Power.WoL.HMACSecret, c.Request().Header.Get("X-GOMI-WOL-Signature")) {
		return c.JSON(gohttp.StatusUnauthorized, map[string]string{"error": "invalid signature"})
	}

	var req machinePowerEventRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.Stage = strings.TrimSpace(req.Stage)
	if req.RequestID == "" || req.Stage == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "requestID and stage are required"})
	}

	result := "success"
	if req.Stage == "command_failed" {
		result = "failure"
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = req.Stage
	}
	details := map[string]string{
		"requestID": req.RequestID,
		"stage":     req.Stage,
	}
	if strings.TrimSpace(req.DaemonVersion) != "" {
		details["daemonVersion"] = strings.TrimSpace(req.DaemonVersion)
	}
	if strings.TrimSpace(req.CreatedAt) != "" {
		details["daemonCreatedAt"] = strings.TrimSpace(req.CreatedAt)
	}

	if s.authStore != nil {
		_ = s.authStore.CreateAuditEvent(c.Request().Context(), auth.AuditEvent{
			ID:        uuid.NewString(),
			Machine:   name,
			Action:    "wol-power-event",
			Actor:     "wol-daemon",
			Result:    result,
			Message:   message,
			Details:   details,
			CreatedAt: time.Now().UTC(),
		})
	}
	return c.JSON(gohttp.StatusOK, map[string]string{"status": "ok"})
}

func verifyPowerEventSignature(body []byte, secret, signature string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}
	signature = strings.TrimPrefix(signature, "sha256=")
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
