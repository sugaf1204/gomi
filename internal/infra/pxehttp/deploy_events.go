package pxehttp

import (
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
	gohttp "net/http"
	"strconv"
	"strings"
	"time"
)

type deployEventRequest struct {
	AttemptID        string          `json:"attemptId,omitempty"`
	Type             string          `json:"type"`
	Source           string          `json:"source,omitempty"`
	Name             string          `json:"name,omitempty"`
	Message          string          `json:"message,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Result           string          `json:"result,omitempty"`
	LogTail          string          `json:"logTail,omitempty"`
	StartedAt        string          `json:"startedAt,omitempty"`
	FinishedAt       string          `json:"finishedAt,omitempty"`
	Timestamp        any             `json:"timestamp,omitempty"`
	DurationMillis   int64           `json:"durationMs,omitempty"`
	MonotonicSeconds float64         `json:"monotonicSeconds,omitempty"`
	CurtinEventType  string          `json:"event_type,omitempty"`
	CurtinOrigin     string          `json:"origin,omitempty"`
	Description      string          `json:"description,omitempty"`
	Details          json.RawMessage `json:"details,omitempty"`
}

func (h *Handler) PXEDeployEvents(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("token is required"))
	}
	queryAttemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, jsonErrorErr(err))
	}
	var req deployEventRequest
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		req.Type = c.FormValue("type")
		req.Source = c.FormValue("source")
		req.Name = c.FormValue("name")
		req.Message = c.FormValue("message")
		req.Reason = c.FormValue("reason")
		req.Result = c.FormValue("result")
		req.LogTail = c.FormValue("logTail")
		req.AttemptID = c.FormValue("attemptId")
		req.StartedAt = c.FormValue("startedAt")
		req.FinishedAt = c.FormValue("finishedAt")
		req.Timestamp = c.FormValue("timestamp")
		if duration := strings.TrimSpace(c.FormValue("durationMs")); duration != "" {
			req.DurationMillis, _ = strconv.ParseInt(duration, 10, 64)
		}
		if monotonic := strings.TrimSpace(c.FormValue("monotonicSeconds")); monotonic != "" {
			req.MonotonicSeconds, _ = strconv.ParseFloat(monotonic, 64)
		}
	} else if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	if req.Source == "" {
		req.Source = strings.TrimSpace(c.QueryParam("source"))
	}
	eventType := strings.ToLower(strings.TrimSpace(req.Type))
	if eventType == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		eventType = strings.ToLower(strings.TrimSpace(req.CurtinEventType))
	}
	if eventType == "" && (strings.TrimSpace(req.Source) != "" || strings.TrimSpace(req.Name) != "") {
		eventType = "timing"
	}
	if eventType == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("type is required"))
	}
	if req.AttemptID != "" && queryAttemptID != "" && strings.TrimSpace(req.AttemptID) != queryAttemptID {
		return c.JSON(gohttp.StatusConflict, jsonError("attempt_id query and body mismatch"))
	}
	if queryAttemptID == "" {
		queryAttemptID = strings.TrimSpace(req.AttemptID)
	}
	if err := validateAttemptParam(target, queryAttemptID); err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}
	now := time.Now().UTC()
	if err := h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.LastSignalAt = &now
		if timing, ok := provisionTimingFromDeployEvent(req, eventType, now); ok {
			m.Provision.Timings = appendProvisionTiming(m.Provision.Timings, timing)
		}
		message := eventMessage(eventType, firstNonEmpty(req.Message, req.Description))
		if !isTimingOnlyDeployEvent(eventType) {
			m.Provision.Message = message
		}
		if eventType == "failed" {
			m.Phase = machine.PhaseError
			m.LastError = firstNonEmpty(req.Reason, req.Message, "deploy failed")
			m.Provision.Active = false
			m.Provision.FinishedAt = &now
			m.Provision.FailureReason = m.LastError
			if logTail := trimLogTail(req.LogTail, maxProvisionFailureLogTailLen); logTail != "" {
				if m.Provision.Artifacts == nil {
					m.Provision.Artifacts = map[string]string{}
				}
				m.Provision.Artifacts[provisionArtifactFailureLogTail] = logTail
			}
		} else if eventType == deployEventImageApplied {
			if m.Provision.Artifacts == nil {
				m.Provision.Artifacts = map[string]string{}
			}
			m.Provision.Artifacts[provisionArtifactImageApplied] = "true"
			m.Provision.Artifacts[provisionArtifactImageAppliedAt] = now.Format(time.RFC3339)
			m.Provision.Message = h.configureBIOSBootOrder(ctx, *m, message)
		}
	}); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, statusResponse{Status: "ok"})
}

func isTimingOnlyDeployEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "timing", "start", "finish":
		return true
	default:
		return false
	}
}

func trimLogTail(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxLen <= 0 {
		return ""
	}
	if len(value) <= maxLen {
		return value
	}
	return value[len(value)-maxLen:]
}

func provisionTimingFromDeployEvent(req deployEventRequest, eventType string, now time.Time) (machine.ProvisionTiming, bool) {
	source := strings.TrimSpace(req.Source)
	if source == "" && strings.TrimSpace(req.CurtinOrigin) != "" {
		source = strings.TrimSpace(req.CurtinOrigin)
	}
	if source == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		source = "curtin"
	}
	if source == "" && eventType == "timing" {
		source = "runner"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		name = strings.TrimSpace(req.CurtinEventType)
	}
	if name == "" && eventType != "" {
		name = eventType
	}
	if source == "" && name == "" && req.DurationMillis <= 0 && req.MonotonicSeconds == 0 && req.StartedAt == "" && req.FinishedAt == "" && req.Timestamp == nil {
		return machine.ProvisionTiming{}, false
	}
	startedAt := parseEventTime(req.StartedAt)
	finishedAt := parseEventTime(req.FinishedAt)
	timestamp := parseEventTimeValue(req.Timestamp)
	if timestamp == nil && startedAt == nil && finishedAt == nil {
		t := now
		timestamp = &t
	}
	if strings.EqualFold(eventType, "start") && startedAt == nil && timestamp != nil {
		startedAt = timestamp
	}
	if strings.EqualFold(eventType, "finish") && finishedAt == nil && timestamp != nil {
		finishedAt = timestamp
	}
	return machine.ProvisionTiming{
		Source:           source,
		Name:             name,
		EventType:        eventType,
		Message:          firstNonEmpty(req.Message, req.Description),
		Result:           strings.TrimSpace(req.Result),
		Timestamp:        timestamp,
		StartedAt:        startedAt,
		FinishedAt:       finishedAt,
		DurationMillis:   req.DurationMillis,
		MonotonicSeconds: req.MonotonicSeconds,
	}, true
}

func appendProvisionTiming(events []machine.ProvisionTiming, next ...machine.ProvisionTiming) []machine.ProvisionTiming {
	for _, event := range next {
		if event.DurationMillis <= 0 && event.FinishedAt != nil {
			for i := len(events) - 1; i >= 0; i-- {
				prev := events[i]
				if prev.StartedAt == nil || prev.FinishedAt != nil {
					continue
				}
				if strings.TrimSpace(prev.Source) != strings.TrimSpace(event.Source) || strings.TrimSpace(prev.Name) != strings.TrimSpace(event.Name) {
					continue
				}
				event.StartedAt = prev.StartedAt
				event.DurationMillis = event.FinishedAt.Sub(*prev.StartedAt).Milliseconds()
				break
			}
		}
		events = append(events, event)
		if len(events) > maxProvisionTimingEvents {
			events = events[len(events)-maxProvisionTimingEvents:]
		}
	}
	return events
}

func serverTiming(name, message, result string, startedAt, finishedAt time.Time, sizeBytes int64) machine.ProvisionTiming {
	duration := finishedAt.Sub(startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	if sizeBytes > 0 {
		message = fmt.Sprintf("%s (%d bytes)", message, sizeBytes)
	}
	return machine.ProvisionTiming{
		Source:         "server",
		Name:           name,
		EventType:      "timing",
		Message:        message,
		Result:         result,
		StartedAt:      &startedAt,
		FinishedAt:     &finishedAt,
		DurationMillis: duration,
	}
}

func parseEventTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		utc := t.UTC()
		return &utc
	}
	if unix, err := strconv.ParseFloat(value, 64); err == nil && unix > 0 {
		sec := int64(unix)
		nsec := int64((unix - float64(sec)) * 1e9)
		t := time.Unix(sec, nsec).UTC()
		return &t
	}
	return nil
}

func parseEventTimeValue(value any) *time.Time {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return parseEventTime(v)
	case float64:
		return parseEventTime(strconv.FormatFloat(v, 'f', -1, 64))
	case json.Number:
		return parseEventTime(string(v))
	default:
		return nil
	}
}

func eventMessage(eventType, message string) string {
	if strings.TrimSpace(message) != "" {
		return message
	}
	if eventType == deployEventImageApplied {
		return "image applied; waiting for target OS first boot"
	}
	return "deploy event: " + eventType
}
