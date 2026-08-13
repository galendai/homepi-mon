package webadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/galendai/homepi-mon/internal/protocol"
)

var canonicalCommandID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type displayControlRequest struct {
	Action          string   `json:"action"`
	PageID          string   `json:"page_id,omitempty"`
	DurationSeconds int      `json:"duration_seconds,omitempty"`
	Enabled         *bool    `json:"enabled,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	ConnectorIDs    []string `json:"connector_ids,omitempty"`
	Text            string   `json:"text,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	Level           int      `json:"level,omitempty"`
}

func (s *Server) handleDisplayControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.KioskControl == nil {
		writeJSONError(w, http.StatusServiceUnavailable, errors.New("kiosk control is unavailable"))
		return
	}
	if s.cfg.DisplayManager == nil {
		writeJSONError(w, http.StatusServiceUnavailable, errors.New("display profile is unavailable"))
		return
	}
	var input displayControlRequest
	if err := decodeStrictJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	deviceID := ""
	var err error
	if targeter, ok := s.cfg.DisplayManager.(interface {
		Target(context.Context) (string, error)
	}); ok {
		deviceID, err = targeter.Target(r.Context())
	} else {
		state, stateErr := s.cfg.DisplayManager.State(r.Context())
		if stateErr == nil {
			deviceID = state.Profile.DeviceID
		} else {
			err = stateErr
		}
	}
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, errors.New("display profile is unavailable"))
		return
	}
	if deviceID == "" {
		writeJSONError(w, http.StatusConflict, errors.New("display device is not configured"))
		return
	}
	request, err := buildDisplayCommand(input)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	status, err := s.cfg.KioskControl.Publish(r.Context(), deviceID, request)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}

func (s *Server) handleDisplayControlStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.KioskControl == nil {
		writeJSONError(w, http.StatusServiceUnavailable, errors.New("kiosk control is unavailable"))
		return
	}
	commandID := r.URL.Path[len("/api/display/control/"):]
	if !canonicalCommandID.MatchString(commandID) {
		writeJSONError(w, http.StatusBadRequest, errors.New("command ID is invalid"))
		return
	}
	status, err := s.cfg.KioskControl.Status(r.Context(), commandID)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func buildDisplayCommand(input displayControlRequest) (protocol.CommandRequest, error) {
	request := protocol.CommandRequest{Priority: protocol.PriorityNormal, TTLSeconds: int(protocol.DefaultCommandTTL.Seconds())}
	var params any
	switch input.Action {
	case "show_page":
		if input.DurationSeconds == 0 {
			input.DurationSeconds = int(protocol.DefaultPageDuration.Seconds())
		}
		if input.IntervalSeconds != 0 || input.Enabled != nil || len(input.ConnectorIDs) != 0 || input.Text != "" || input.Severity != "" || input.Level != 0 {
			return request, errors.New("show_page contains unsupported fields")
		}
		request.Kind, params = protocol.CommandShowPage, protocol.PageCommandParams{PageID: input.PageID, DurationSeconds: input.DurationSeconds}
	case "next_page", "previous_page":
		if input.DurationSeconds == 0 {
			input.DurationSeconds = int(protocol.DefaultPageDuration.Seconds())
		}
		if input.PageID != "" || input.IntervalSeconds != 0 || input.Enabled != nil || len(input.ConnectorIDs) != 0 || input.Text != "" || input.Severity != "" || input.Level != 0 {
			return request, errors.New("page step contains unsupported fields")
		}
		request.Kind = protocol.CommandKind(input.Action)
		params = protocol.StepPageCommandParams{DurationSeconds: input.DurationSeconds}
	case "set_rotation":
		if input.Enabled == nil {
			return request, errors.New("set_rotation requires enabled")
		}
		if input.PageID != "" || len(input.ConnectorIDs) != 0 || input.Text != "" || input.Severity != "" || input.Level != 0 {
			return request, errors.New("set_rotation contains unsupported fields")
		}
		request.Kind, params = protocol.CommandSetRotation, protocol.RotationCommandParams{Enabled: *input.Enabled, IntervalSeconds: input.IntervalSeconds, DurationSeconds: input.DurationSeconds}
	case "refresh_data":
		if input.PageID != "" || input.DurationSeconds != 0 || input.IntervalSeconds != 0 || input.Enabled != nil || input.Text != "" || input.Severity != "" || input.Level != 0 {
			return request, errors.New("refresh_data contains unsupported fields")
		}
		request.Kind, params = protocol.CommandRefreshData, protocol.RefreshCommandParams{ConnectorIDs: input.ConnectorIDs}
	case "show_message":
		if input.DurationSeconds == 0 {
			input.DurationSeconds = int(protocol.DefaultPageDuration.Seconds())
		}
		if input.PageID != "" || input.IntervalSeconds != 0 || input.Enabled != nil || len(input.ConnectorIDs) != 0 || input.Level != 0 {
			return request, errors.New("show_message contains unsupported fields")
		}
		request.Kind, request.Priority, params = protocol.CommandShowMessage, protocol.PriorityHigh, protocol.MessageCommandParams{Text: input.Text, Severity: input.Severity, DurationSeconds: input.DurationSeconds}
	case "set_brightness":
		if input.PageID != "" || input.DurationSeconds != 0 || input.IntervalSeconds != 0 || input.Enabled != nil || len(input.ConnectorIDs) != 0 || input.Text != "" || input.Severity != "" {
			return request, errors.New("set_brightness contains unsupported fields")
		}
		request.Kind, params = protocol.CommandSetBrightness, protocol.BrightnessCommandParams{Level: input.Level}
	default:
		return request, errors.New("action is not allowed")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return request, errors.New("command parameters are invalid")
	}
	request.Params = raw
	if err := protocol.ValidateCommandRequest(request); err != nil {
		return request, errors.New("command parameters are invalid: " + protocol.CommandErrorCode(err))
	}
	return request, nil
}
