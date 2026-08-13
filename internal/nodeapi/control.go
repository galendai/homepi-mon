package nodeapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/protocol"
)

func (s *Server) handlePublishCommand(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}
	device := r.PathValue("device")
	s.mu.RLock()
	_, known := s.devices[device]
	s.mu.RUnlock()
	if !known {
		writeError(w, http.StatusNotFound, "unknown_device", "target device is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.MaxCommandBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_size", "command request is too large")
		return
	}
	request, err := protocol.DecodeCommandRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.CommandErrorCode(err), "command request was rejected")
		return
	}
	if request.Kind == protocol.CommandRefreshData {
		params, decodeErr := protocol.DecodeCommandParams[protocol.RefreshCommandParams](request.Params)
		if decodeErr != nil || !s.knownConnectors(params.ConnectorIDs) {
			writeError(w, http.StatusBadRequest, "invalid_param", "refresh connector is not configured")
			return
		}
	}
	entry, err := s.commands.Publish(device, request)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "queue_unavailable", "command could not be queued")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, commandResponse(entry))
}

func (s *Server) handleCommandStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeControl(w, r) {
		return
	}
	entry, err := s.commands.Get(r.PathValue("command"))
	if errors.Is(err, commandbus.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "command was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "queue_unavailable", "command state is unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, commandResponse(entry))
}

func (s *Server) authorizeControl(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != "" || !localControlRequest(r) {
		writeError(w, http.StatusForbidden, "local_only", "command control is available only on the node host")
		return false
	}
	presented := bearerToken(r)
	if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(s.controlToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="homepi-control"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "control credential is missing or invalid")
		return false
	}
	return true
}

func localControlRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	remote := net.ParseIP(strings.Trim(host, "[]"))
	if remote == nil {
		return false
	}
	if remote.IsLoopback() {
		return true
	}
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return false
	}
	localHost, _, err := net.SplitHostPort(local.String())
	if err != nil {
		return false
	}
	localIP := net.ParseIP(strings.Trim(localHost, "[]"))
	return localIP != nil && !localIP.IsUnspecified() && remote.Equal(localIP)
}

func (s *Server) knownConnectors(ids []string) bool {
	if len(ids) == 0 {
		return len(s.connectors) > 0
	}
	for _, id := range ids {
		if !s.connectors[id] {
			return false
		}
	}
	return true
}

type controlCommandResponse struct {
	CommandID string                 `json:"command_id"`
	DeviceID  string                 `json:"device_id"`
	Kind      protocol.CommandKind   `json:"kind"`
	Sequence  uint64                 `json:"sequence"`
	IssuedAt  string                 `json:"issued_at"`
	ExpiresAt string                 `json:"expires_at"`
	Result    protocol.CommandResult `json:"result"`
}

func commandResponse(entry commandbus.Entry) controlCommandResponse {
	return controlCommandResponse{
		CommandID: entry.Command.CommandID, DeviceID: entry.Command.DeviceID,
		Kind: entry.Command.Kind, Sequence: entry.Command.Sequence,
		IssuedAt:  entry.Command.IssuedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		ExpiresAt: entry.Command.ExpiresAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		Result:    entry.Result,
	}
}

func encodeControlBody(request protocol.CommandRequest) []byte {
	body, _ := json.Marshal(request)
	return body
}
