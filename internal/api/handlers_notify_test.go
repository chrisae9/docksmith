package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/notify"
	"github.com/chis/docksmith/internal/storage"
)

type notifyTestEnv struct {
	mux   *http.ServeMux
	store *storage.SQLiteStorage
}

func newNotifyTestEnv(t *testing.T) *notifyTestEnv {
	t.Helper()
	st, err := storage.NewSQLiteStorage(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := &Server{
		notifyStore: st,
		notifier:    notify.New(st, st, nil, nil, notify.ShoutrrrSender{}, "host", 5*time.Minute),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/notifications/settings", s.handleNotifySettingsGet)
	mux.HandleFunc("PUT /api/notifications/settings", s.handleNotifySettingsPut)
	mux.HandleFunc("POST /api/notifications/validate-cron", s.handleNotifyValidateCron)
	mux.HandleFunc("GET /api/notifications/services", s.handleNotifyServicesList)
	mux.HandleFunc("POST /api/notifications/services", s.handleNotifyServiceCreate)
	mux.HandleFunc("PUT /api/notifications/services/{id}", s.handleNotifyServiceUpdate)
	mux.HandleFunc("DELETE /api/notifications/services/{id}", s.handleNotifyServiceDelete)
	mux.HandleFunc("POST /api/notifications/services/{id}/test", s.handleNotifyServiceTest)
	return &notifyTestEnv{mux: mux, store: st}
}

func (e *notifyTestEnv) do(method, path, body string) (int, string) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

func (e *notifyTestEnv) settings(t *testing.T) notify.Settings {
	t.Helper()
	code, body := e.do("GET", "/api/notifications/settings", "")
	if code != 200 {
		t.Fatalf("GET settings: %d %s", code, body)
	}
	var resp struct {
		Data struct {
			Settings notify.Settings `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	return resp.Data.Settings
}

func TestNotifySettings_RejectsTooFrequentCron(t *testing.T) {
	e := newNotifyTestEnv(t)
	old := e.settings(t)

	upd := old
	upd.SummaryCron = "*/1 * * * *"
	b, _ := json.Marshal(upd)
	code, body := e.do("PUT", "/api/notifications/settings", string(b))
	if code != http.StatusBadRequest {
		t.Fatalf("PUT = %d, want 400 (%s)", code, body)
	}
	if got := e.settings(t).SummaryCron; got != old.SummaryCron {
		t.Errorf("cron changed to %q after rejected PUT, want %q", got, old.SummaryCron)
	}
}

func TestNotifyValidateCron(t *testing.T) {
	e := newNotifyTestEnv(t)
	code, body := e.do("POST", "/api/notifications/validate-cron", `{"cron":"0 9 * * *"}`)
	if code != 200 || !strings.Contains(body, `"valid":true`) {
		t.Errorf("valid cron: %d %s", code, body)
	}
	code, body = e.do("POST", "/api/notifications/validate-cron", `{"cron":"*/1 * * * *"}`)
	if code != 200 || !strings.Contains(body, `"valid":false`) {
		t.Errorf("too frequent cron: %d %s", code, body)
	}
}

func TestNotifyServices_MaskedRoundTrip(t *testing.T) {
	e := newNotifyTestEnv(t)
	create := `{"name":"mail","type":"smtp","enabled":true,
		"config":{"host":"smtp.a.test","from":"a@a.test","to":"b@a.test","password":"hunter2"}}`
	code, body := e.do("POST", "/api/notifications/services", create)
	if code != 200 {
		t.Fatalf("POST = %d %s", code, body)
	}
	bodies := []string{body}

	code, body = e.do("GET", "/api/notifications/services", "")
	if code != 200 || !strings.Contains(body, "********") {
		t.Fatalf("GET = %d %s", code, body)
	}
	bodies = append(bodies, body)

	rows, err := e.store.ListNotifyServices(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %d", err, len(rows))
	}
	id := rows[0].ID

	put := `{"name":"mail","type":"smtp","enabled":true,
		"config":{"host":"smtp.b.test","from":"a@a.test","to":"b@a.test","password":"********"}}`
	code, body = e.do("PUT", "/api/notifications/services/"+strconv.FormatInt(id, 10), put)
	if code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	bodies = append(bodies, body)

	got, found, err := e.store.GetNotifyService(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("get: %v %v", err, found)
	}
	if got.Config["password"] != "hunter2" {
		t.Errorf("stored password = %q, want hunter2", got.Config["password"])
	}
	if got.Config["host"] != "smtp.b.test" {
		t.Errorf("host = %q, want smtp.b.test", got.Config["host"])
	}
	for _, b := range bodies {
		if strings.Contains(b, "hunter2") {
			t.Errorf("response leaks secret: %s", b)
		}
	}
}

func TestNotifySettings_EventTimingValues(t *testing.T) {
	e := newNotifyTestEnv(t)
	settings := func(timing string) string {
		return `{"server_name":"h","min_level":"patch","summary_cron":"0 8 * * *","timing":` + timing + `}`
	}
	if code, resp := e.do("PUT", "/api/notifications/settings", settings(`{"new_version":"weekly"}`)); code != http.StatusBadRequest {
		t.Errorf("bad timing: %d %s", code, resp)
	}
	if code, resp := e.do("PUT", "/api/notifications/settings", settings(`{"bogus":"off"}`)); code != http.StatusBadRequest {
		t.Errorf("unknown event: %d %s", code, resp)
	}
	code, resp := e.do("PUT", "/api/notifications/settings", settings(`{"compose_mismatch":"off"}`))
	if code != http.StatusOK || !strings.Contains(resp, `"compose_mismatch":"off"`) {
		t.Errorf("off timing: %d %s", code, resp)
	}
}

func TestNotifyServices_UnknownAndInvalidID(t *testing.T) {
	e := newNotifyTestEnv(t)
	body := `{"name":"n","type":"ntfy","enabled":true,"config":{"topic":"t"}}`
	if code, _ := e.do("PUT", "/api/notifications/services/999", body); code != http.StatusNotFound {
		t.Errorf("PUT unknown = %d, want 404", code)
	}
	if code, _ := e.do("DELETE", "/api/notifications/services/999", ""); code != http.StatusNotFound {
		t.Errorf("DELETE unknown = %d, want 404", code)
	}
	if code, _ := e.do("POST", "/api/notifications/services/999/test", ""); code != http.StatusNotFound {
		t.Errorf("test unknown = %d, want 404", code)
	}
	if code, _ := e.do("DELETE", "/api/notifications/services/abc", ""); code != http.StatusBadRequest {
		t.Errorf("DELETE abc = %d, want 400", code)
	}
}

func TestNotify_UnavailableWithoutNotifier(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleNotifySettingsGet(rec, httptest.NewRequest("GET", "/api/notifications/settings", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", rec.Code)
	}
}

func TestNotifyServices_CreateNeverStoresMask(t *testing.T) {
	e := newNotifyTestEnv(t)
	code, body := e.do("POST", "/api/notifications/services",
		`{"name":"hook","type":"webhook","enabled":true,"config":{"url":"********"}}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "missing required fields: url") {
		t.Fatalf("POST masked required secret = %d %s, want 400 missing url", code, body)
	}

	code, body = e.do("POST", "/api/notifications/services",
		`{"name":"mail","type":"smtp","enabled":true,
		"config":{"host":"smtp.a.test","from":"a@a.test","to":"b@a.test","password":"********"}}`)
	if code != 200 {
		t.Fatalf("POST = %d %s", code, body)
	}
	rows, err := e.store.ListNotifyServices(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %d", err, len(rows))
	}
	if v, ok := rows[0].Config["password"]; ok {
		t.Fatalf("masked password stored as %q", v)
	}
}

func TestNotifyServices_TypeChangeDropsMaskedValues(t *testing.T) {
	e := newNotifyTestEnv(t)
	code, body := e.do("POST", "/api/notifications/services",
		`{"name":"mail","type":"smtp","enabled":true,
		"config":{"host":"smtp.a.test","from":"a@a.test","to":"b@a.test","password":"hunter2"}}`)
	if code != 200 {
		t.Fatalf("POST = %d %s", code, body)
	}
	rows, _ := e.store.ListNotifyServices(t.Context())
	id := strconv.FormatInt(rows[0].ID, 10)

	// The UI resubmits the masked smtp fields alongside the new ntfy ones.
	code, body = e.do("PUT", "/api/notifications/services/"+id,
		`{"name":"mail","type":"ntfy","enabled":true,
		"config":{"topic":"t","password":"********","smtp_password":"********","host":"ntfy.a.test"}}`)
	if code != 200 {
		t.Fatalf("PUT = %d %s", code, body)
	}
	got, _, _ := e.store.GetNotifyService(t.Context(), rows[0].ID)
	for k, v := range got.Config {
		if v == notify.MaskedSecret || v == "hunter2" {
			t.Errorf("config[%s] = %q after type change", k, v)
		}
	}
	if got.Config["topic"] != "t" || got.Config["host"] != "ntfy.a.test" {
		t.Errorf("config = %v", got.Config)
	}
}
