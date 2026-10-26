package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/notify"
	"github.com/chis/docksmith/internal/storage"
)

const nextSummaryCount = 3

// notifyServiceRequest is the body of POST/PUT /api/notifications/services.
type notifyServiceRequest struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

// notifyServiceResponse is a service as returned to clients; Config is always masked.
type notifyServiceResponse struct {
	ID            int64             `json:"id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Enabled       bool              `json:"enabled"`
	Config        map[string]string `json:"config"`
	LastSuccessAt *time.Time        `json:"last_success_at,omitempty"`
	LastErrorAt   *time.Time        `json:"last_error_at,omitempty"`
	LastSummaryAt *time.Time        `json:"last_summary_at,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
}

func toNotifyServiceResponse(row storage.NotifyServiceRow) notifyServiceResponse {
	return notifyServiceResponse{
		ID: row.ID, Name: row.Name, Type: row.Type, Enabled: row.Enabled,
		Config:        notify.MaskConfig(row.Type, row.Config),
		LastSuccessAt: row.LastSuccessAt, LastErrorAt: row.LastErrorAt,
		LastSummaryAt: row.LastSummaryAt, LastError: row.LastError,
	}
}

// notifyAvailable responds 503 and returns false when notifications are unsupported.
func (s *Server) notifyAvailable(w http.ResponseWriter) bool {
	if s.notifier == nil || s.notifyStore == nil {
		RespondError(w, http.StatusServiceUnavailable, errors.New("notifications are not available"))
		return false
	}
	return true
}

func formatTimes(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Format(time.RFC3339))
	}
	return out
}

func (s *Server) notifySettingsPayload() map[string]any {
	settings := s.notifier.Settings()
	tzName, tzWarning := notify.TimezoneInfo()
	next, err := notify.NextFires(settings.SummaryCron, time.Local, time.Now(), nextSummaryCount)
	if err != nil {
		next = nil
	}
	return map[string]any{
		"settings":         settings,
		"timezone":         tzName,
		"timezone_warning": tzWarning,
		"check_interval":   s.notifier.CheckInterval().String(),
		"next_summaries":   formatTimes(next),
	}
}

func (s *Server) handleNotifySettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	RespondSuccess(w, s.notifySettingsPayload())
}

func (s *Server) handleNotifySettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	var settings notify.Settings
	if !decodeJSONRequest(w, r, &settings) {
		return
	}
	if err := s.notifier.UpdateSettings(r.Context(), settings); err != nil {
		RespondBadRequest(w, err)
		return
	}
	RespondSuccess(w, s.notifySettingsPayload())
}

func (s *Server) handleNotifyValidateCron(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	var req struct {
		Cron string `json:"cron"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	expr := strings.TrimSpace(req.Cron)
	now := time.Now()
	resp := map[string]any{"valid": true, "error": "", "next": []string{}}
	if expr == "" {
		resp["valid"] = false
		resp["error"] = "summary schedule is required"
	} else if err := notify.ValidateSchedule(expr, s.notifier.CheckInterval(), time.Local, now); err != nil {
		resp["valid"] = false
		resp["error"] = err.Error()
	} else if next, err := notify.NextFires(expr, time.Local, now, nextSummaryCount); err == nil {
		resp["next"] = formatTimes(next)
	}
	RespondSuccess(w, resp)
}

func (s *Server) handleNotifyServicesList(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	rows, err := s.notifyStore.ListNotifyServices(r.Context())
	if err != nil {
		RespondInternalError(w, errors.New("list notification services failed"))
		return
	}
	out := make([]notifyServiceResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toNotifyServiceResponse(row))
	}
	RespondSuccess(w, out)
}

// validateNotifyServiceRequest checks everything except the config.
func validateNotifyServiceRequest(req *notifyServiceRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (s *Server) handleNotifyServiceCreate(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	var req notifyServiceRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if err := validateNotifyServiceRequest(&req); err != nil {
		RespondBadRequest(w, err)
		return
	}
	// There is nothing stored to restore a masked secret from: drop it, so a
	// required secret fails validation instead of storing the literal mask.
	req.Config = notify.MergeSecrets(req.Type, req.Config, nil)
	if err := notify.ValidateServiceConfig(req.Type, req.Config); err != nil {
		RespondBadRequest(w, err)
		return
	}
	row := storage.NotifyServiceRow{
		Name: req.Name, Type: req.Type, Enabled: req.Enabled, Config: req.Config,
	}
	id, err := s.notifyStore.SaveNotifyService(r.Context(), row)
	if err != nil {
		RespondInternalError(w, errors.New("save notification service failed"))
		return
	}
	s.respondNotifyService(w, r, id)
}

func (s *Server) handleNotifyServiceUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	id, ok := parseNotifyServiceID(w, r)
	if !ok {
		return
	}
	var req notifyServiceRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	stored, found, err := s.notifyStore.GetNotifyService(r.Context(), id)
	if err != nil {
		RespondInternalError(w, errors.New("load notification service failed"))
		return
	}
	if !found {
		RespondNotFound(w, fmt.Errorf("notification service %d not found", id))
		return
	}
	if err := validateNotifyServiceRequest(&req); err != nil {
		RespondBadRequest(w, err)
		return
	}
	// Stored secrets only carry over when the service type is unchanged.
	var storedCfg map[string]string
	if stored.Type == req.Type {
		storedCfg = stored.Config
	}
	merged := notify.MergeSecrets(req.Type, req.Config, storedCfg)
	if stored.Type != req.Type {
		// Masked values of the old type's secrets may be resubmitted under
		// fields that are not secrets of the new type; never store the mask.
		for k, v := range merged {
			if v == notify.MaskedSecret {
				delete(merged, k)
			}
		}
	}
	if err := notify.ValidateServiceConfig(req.Type, merged); err != nil {
		RespondBadRequest(w, err)
		return
	}
	row := stored
	row.Name, row.Type, row.Enabled, row.Config = req.Name, req.Type, req.Enabled, merged
	if _, err := s.notifyStore.SaveNotifyService(r.Context(), row); err != nil {
		RespondInternalError(w, errors.New("save notification service failed"))
		return
	}
	s.respondNotifyService(w, r, id)
}

func (s *Server) respondNotifyService(w http.ResponseWriter, r *http.Request, id int64) {
	row, found, err := s.notifyStore.GetNotifyService(r.Context(), id)
	if err != nil || !found {
		RespondInternalError(w, errors.New("load notification service failed"))
		return
	}
	RespondSuccess(w, toNotifyServiceResponse(row))
}

func (s *Server) handleNotifyServiceDelete(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	id, ok := parseNotifyServiceID(w, r)
	if !ok {
		return
	}
	if !s.notifyServiceExists(w, r, id) {
		return
	}
	if err := s.notifyStore.DeleteNotifyService(r.Context(), id); err != nil {
		RespondInternalError(w, errors.New("delete notification service failed"))
		return
	}
	RespondSuccess(w, map[string]any{"deleted": true})
}

func (s *Server) handleNotifyServiceTest(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	id, ok := parseNotifyServiceID(w, r)
	if !ok {
		return
	}
	if !s.notifyServiceExists(w, r, id) {
		return
	}
	// SendTest returns an already-redacted error; pass it through unwrapped.
	if err := s.notifier.Dispatcher().SendTest(r.Context(), id); err != nil {
		RespondBadRequest(w, err)
		return
	}
	RespondSuccess(w, map[string]any{"sent": true})
}

// notifyServiceExists responds 404/500 and returns false when the service can't be used.
func (s *Server) notifyServiceExists(w http.ResponseWriter, r *http.Request, id int64) bool {
	_, found, err := s.notifyStore.GetNotifyService(r.Context(), id)
	if err != nil {
		RespondInternalError(w, errors.New("load notification service failed"))
		return false
	}
	if !found {
		RespondNotFound(w, fmt.Errorf("notification service %d not found", id))
		return false
	}
	return true
}

func parseNotifyServiceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		RespondBadRequest(w, errors.New("invalid service id"))
		return 0, false
	}
	return id, true
}
