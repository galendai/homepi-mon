package webadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/galendai/homepi-mon/internal/configtx"
)

// apiState is the per-Server cache of the in-flight draft. The
// browser edits the draft via /api/draft; the server stores the
// most recent value so the next /api/draft call returns the same
// view.
type apiState struct {
	svc   *configtx.Service
	mu    chan struct{}
	draft *configtx.Draft
}

func newAPI(svc *configtx.Service) *apiState {
	return &apiState{svc: svc, mu: make(chan struct{}, 1)}
}

// withDraft holds the API-level semaphore for the complete request
// operation. Draft also protects its own state, but this wider boundary
// keeps a response consistent with the mutation that produced it.
func (a *apiState) withDraft(ctx context.Context, fn func(*configtx.Draft) error) error {
	a.mu <- struct{}{}
	defer func() { <-a.mu }()
	if a.draft == nil {
		d, err := a.svc.OpenDraft(ctx)
		if err != nil {
			return err
		}
		a.draft = d
	}
	return fn(a.draft)
}

func (a *apiState) draftSnapshot() *configtx.Draft {
	a.mu <- struct{}{}
	defer func() { <-a.mu }()
	return a.draft
}

// statusResponse is the JSON payload of /api/status. Secret values
// never appear; secret refs are masked.
type statusResponse struct {
	Node       nodeStatus       `json:"node"`
	Runtime    RuntimeSnapshot  `json:"runtime"`
	Providers  []providerStatus `json:"providers"`
	Display    displayStatus    `json:"display"`
	LastApply  lastApplyStatus  `json:"last_apply"`
	ServerTime string           `json:"server_time"`
}

type nodeStatus struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ListenAddr    string `json:"listen_addr"`
	SecretBackend string `json:"secret_backend"`
	Revision      string `json:"revision"`
	ProviderCount int    `json:"provider_count"`
	DeviceCount   int    `json:"device_count"`
}

type providerStatus struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Label         string `json:"label"`
	Region        string `json:"region"`
	BaseURL       string `json:"base_url"`
	Interval      string `json:"interval"`
	StaleAfter    string `json:"stale_after"`
	Enabled       bool   `json:"enabled"`
	SecretRef     string `json:"secret_ref,omitempty"`
	SecretPresent bool   `json:"secret_present"`
	AuthFile      string `json:"auth_file,omitempty"`
	Pending       bool   `json:"pending"`
	Configured    bool   `json:"configured"`
}

type displayStatus struct {
	Connected      bool   `json:"connected"`
	Theme          string `json:"theme,omitempty"`
	LatestSnapshot string `json:"latest_snapshot,omitempty"`
	SourceEpoch    string `json:"source_epoch,omitempty"`
	SnapshotVer    uint64 `json:"snapshot_version,omitempty"`
	Note           string `json:"note,omitempty"`
}

type lastApplyStatus struct {
	At     string `json:"at,omitempty"`
	Status string `json:"status,omitempty"`
	Note   string `json:"note,omitempty"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	node, err := s.api.svc.Status(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	rawStatuses, err := s.api.svc.ProviderStatuses(r.Context(), s.api.draftSnapshot())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	providers := make([]providerStatus, 0, len(rawStatuses))
	for _, p := range rawStatuses {
		providers = append(providers, providerStatus{
			ID:            p.ID,
			Type:          p.Type,
			Label:         p.AccountLabel,
			Region:        p.Region,
			BaseURL:       p.BaseURL,
			Interval:      p.Interval,
			StaleAfter:    p.StaleAfter,
			Enabled:       p.Enabled,
			SecretRef:     p.SecretRef,
			SecretPresent: p.SecretPresent,
			AuthFile:      p.AuthFile,
			Pending:       p.Pending,
			Configured:    p.Configured,
		})
	}
	dsp := DisplaySnapshot{Connected: false, Note: "Display status source not registered"}
	if s.cfg.DisplayStatus != nil {
		if current, displayErr := s.cfg.DisplayStatus(); displayErr == nil {
			dsp = current
		} else {
			dsp.Note = displayErr.Error()
		}
	}
	runtimeStatus := RuntimeSnapshot{
		State:   "unavailable",
		Message: "Runtime version detection is not configured.",
	}
	if s.cfg.RuntimeStatus != nil {
		runtimeStatus = s.cfg.RuntimeStatus(r.Context())
	}
	resp := statusResponse{
		Node: nodeStatus{
			ID:            node.NodeID,
			Label:         node.NodeLabel,
			ListenAddr:    node.ListenAddr,
			SecretBackend: node.SecretBackend,
			Revision:      node.Revision,
			ProviderCount: node.ProviderCount,
			DeviceCount:   node.DeviceCount,
		},
		Runtime:   runtimeStatus,
		Providers: providers,
		Display: displayStatus{
			Connected:      dsp.Connected,
			Theme:          dsp.Theme,
			LatestSnapshot: formatTime(dsp.LatestSnapshot),
			SourceEpoch:    dsp.SourceEpoch,
			SnapshotVer:    dsp.SnapshotVer,
			Note:           dsp.Note,
		},
		LastApply: lastApplyStatus{
			At:     formatTime(node.LastApplyAt),
			Status: node.LastApplyState,
			Note:   node.LastApplyNote,
		},
		ServerTime: s.cfg.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var payload map[string]any
		if err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
			payload = draftPayload(d)
			return nil
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodPost, http.MethodPut:
		var edit configtx.ProviderEdit
		if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
			return
		}
		var payload map[string]any
		err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
			if err := d.EditProvider(edit); err != nil {
				return err
			}
			payload = draftPayload(d)
			return nil
		})
		if err != nil {
			writeJSONError(w, statusForEditError(err), err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
			return
		}
		var payload map[string]any
		err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
			if _, err := d.DeleteProvider(body.ID); err != nil {
				return err
			}
			payload = draftPayload(d)
			return nil
		})
		if err != nil {
			writeJSONError(w, statusForEditError(err), err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var diff []configtx.DiffEntry
	if err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
		diff = d.Diff()
		return nil
	}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

type testRequest struct {
	ID string `json:"id"`
}

type testResponse struct {
	ProviderID   string `json:"provider_id"`
	ProviderType string `json:"provider_type"`
	MetricCount  int    `json:"metric_count"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	Class        string `json:"class"`
	Message      string `json:"message,omitempty"`
	RetryAfter   int    `json:"retry_after_seconds,omitempty"`
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	var res configtx.TestResult
	err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
		var testErr error
		res, testErr = d.TestProvider(r.Context(), req.ID)
		return testErr
	})
	resp := testResponse{
		ProviderID:   res.ProviderID,
		ProviderType: res.ProviderType,
		MetricCount:  res.MetricCount,
		ElapsedMS:    res.Elapsed.Milliseconds(),
		Class:        string(res.Class),
		Message:      res.Message,
		RetryAfter:   res.RetryAfter,
	}
	if err != nil {
		writeJSON(w, http.StatusOK, resp) // surface the classification, not the error chain
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type applyRequest struct{}

type applyResponse struct {
	Revision         string              `json:"revision"`
	PersistedAt      string              `json:"persisted_at"`
	AddedProviders   []string            `json:"added_providers"`
	RemovedProviders []string            `json:"removed_providers"`
	ModifiedFields   map[string][]string `json:"modified_fields"`
	OldSecretsPruned []string            `json:"old_secrets_pruned"`
	Restarted        bool                `json:"restarted"`
	Healthy          bool                `json:"healthy"`
	StepLog          []StepRecord        `json:"step_log"`
	Error            string              `json:"error,omitempty"`
	ErrorStep        string              `json:"error_step,omitempty"`
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if s.cfg.Restart == nil || s.cfg.HealthCheck == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			errors.New("service restart and health check are not configured"))
		return
	}
	var res configtx.ApplyResult
	var applyErr error
	err := s.api.withDraft(r.Context(), func(d *configtx.Draft) error {
		res, applyErr = s.api.svc.Apply(r.Context(), d, configtx.ApplyOptions{
			Restart: s.cfg.Restart, HealthCheck: s.cfg.HealthCheck,
			RequireProviderTests: true,
		})
		if applyErr == nil || errors.Is(applyErr, configtx.ErrRevisionConflict) {
			s.api.draft = nil
		}
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	resp := applyResponse{
		Revision:         res.Revision,
		PersistedAt:      formatTime(res.PersistedAt),
		AddedProviders:   res.AddedProviders,
		RemovedProviders: res.RemovedProviders,
		ModifiedFields:   res.ModifiedFields,
		OldSecretsPruned: res.OldSecretsPruned,
		Restarted:        res.Restarted,
		Healthy:          res.Healthy,
		StepLog:          res.StepLog,
	}
	if applyErr != nil {
		resp.Error = applyErr.Error()
		var applyErrType *configtx.ApplyError
		if errors.As(applyErr, &applyErrType) {
			resp.ErrorStep = string(applyErrType.Step)
		}
		writeJSON(w, http.StatusConflict, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type typeInfo struct {
	TypeID           string   `json:"type"`
	Label            string   `json:"label"`
	Provider         string   `json:"provider"`
	RequiresSecret   bool     `json:"requires_secret"`
	SecretFieldLabel string   `json:"secret_field_label,omitempty"`
	RequiresAuthFile bool     `json:"requires_auth_file"`
	AuthFileField    string   `json:"auth_file_field,omitempty"`
	MinIntervalMS    int64    `json:"min_interval_ms"`
	MinStaleAfterMS  int64    `json:"min_stale_after_ms"`
	SupportedRegions []string `json:"supported_regions"`
	DefaultRegion    string   `json:"default_region"`
	DefaultBaseURL   string   `json:"default_base_url,omitempty"`
	MockFixtureOnly  bool     `json:"mock_fixture_only"`
	MockFixtureLabel string   `json:"mock_fixture_label,omitempty"`
	Description      string   `json:"description"`
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Build the response from the registered providermeta so the UI
	// can render the type picker without hard-coding labels.
	resp := make([]typeInfo, 0)
	// The webadmin package does not import providermeta directly to
	// keep the dep graph narrow; the Service's Status call returns
	// enough info for the Overview, and the type list is small
	// enough to embed statically through the static handler.
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": s.csrfTok})
}

type draftProvider struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	AccountLabel string `json:"account_label"`
	Region       string `json:"region"`
	BaseURL      string `json:"base_url,omitempty"`
	Interval     string `json:"interval"`
	StaleAfter   string `json:"stale_after"`
	Enabled      bool   `json:"enabled"`
	SecretRef    string `json:"secret_ref,omitempty"`
	AuthFile     string `json:"auth_file,omitempty"`
	MockFixture  string `json:"mock_fixture,omitempty"`
}

type draftDevice struct {
	ID                string   `json:"id"`
	SubscribedMetrics []string `json:"subscribed_metrics,omitempty"`
}

func draftPayload(d *configtx.Draft) map[string]any {
	pending := d.Pending()
	providers := make([]draftProvider, 0, len(pending.Providers))
	for _, p := range pending.Providers {
		providers = append(providers, draftProvider{
			ID: p.ID, Type: p.Type, AccountLabel: p.AccountLabel,
			Region: p.Region, BaseURL: p.BaseURL, Interval: p.Interval,
			StaleAfter: p.StaleAfter, Enabled: p.IsEnabled(),
			SecretRef: maskSecretRef(p.SecretRef), AuthFile: p.AuthFile,
			MockFixture: p.MockFixture,
		})
	}
	devices := make([]draftDevice, 0, len(pending.Devices))
	for _, device := range pending.Devices {
		devices = append(devices, draftDevice{
			ID: device.ID, SubscribedMetrics: append([]string(nil), device.SubscribedMetrics...),
		})
	}
	return map[string]any{
		"revision":    d.Revision(),
		"has_pending": d.HasPending(),
		"diff":        d.Diff(),
		"providers":   providers,
		"devices":     devices,
	}
}

func maskSecretRef(ref string) string {
	if ref == "" {
		return ""
	}
	const prefix = "keyring:"
	const tail = 6
	if len(ref) <= len(prefix)+tail {
		return prefix + "..."
	}
	return prefix + "..." + ref[len(ref)-tail:]
}

func statusForEditError(err error) int {
	switch {
	case errors.Is(err, configtx.ErrInvalidDraft):
		return http.StatusBadRequest
	case errors.Is(err, configtx.ErrShadowedProvider):
		return http.StatusConflict
	case errors.Is(err, configtx.ErrSecretOversize):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// StepRecord is re-exported from configtx so the webadmin client
// does not have to import it.
type StepRecord = configtx.StepRecord
