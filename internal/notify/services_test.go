package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slackFixtureURL is Slack's documented placeholder webhook, assembled so secret
// scanners don't flag the literal.
var slackFixtureURL = "https://hooks.slack.com/services/" + "T00000000/B00000000/" + strings.Repeat("X", 24)

var sampleConfigs = map[string]map[string]string{
	"smtp":     {"host": "smtp.example.com", "port": "587", "username": "u@example.com", "password": "p@ss/w:rd#1", "encryption": "starttls", "from": "docksmith@example.com", "to": "a@example.com,b@example.com"},
	"ntfy":     {"host": "ntfy.example.com", "topic": "docksmith", "token": "tk_abc123", "username": "bob", "password": "p@ss w/rd"},
	"discord":  {"webhook_url": "https://discord.com/api/webhooks/123456789/AbCdEf-gh_ij"},
	"telegram": {"token": "123456:ABC-DEF1234ghIkl", "chats": "@chan1,12345"},
	"pushover": {"token": "azGDORePK8gMaC0QOYAMyEEuzJnyUi", "user_key": "uQiRzpo4DXghDmr9QzzfQu27cmVRsG"},
	"gotify":   {"host": "gotify.example.com:8080", "token": "AbCdEfGhIjKlMnO"},
	"slack":    {"webhook_url": slackFixtureURL},
	"webhook":  {"url": "https://example.com/hook/secretpath?k=v"},
	"advanced": {"url": "ntfy://ntfy.sh/mytopic"},
}

func TestBuildURL_AllTypesParse(t *testing.T) {
	for _, typ := range ServiceTypes {
		t.Run(typ, func(t *testing.T) {
			cfg, ok := sampleConfigs[typ]
			require.True(t, ok)
			u, err := BuildURL(typ, cfg)
			require.NoError(t, err)
			_, err = shoutrrr.CreateSender(u)
			require.NoError(t, err, u)
		})
	}
}

func TestBuildURL_SMTPEscapesPassword(t *testing.T) {
	u, err := BuildURL("smtp", sampleConfigs["smtp"])
	require.NoError(t, err)
	assert.NotContains(t, u, "p@ss/w:rd#1")
	assert.Contains(t, u, "usehtml=yes")
}

func TestMergeSecrets_KeepsStoredWhenMasked(t *testing.T) {
	got := MergeSecrets("smtp",
		map[string]string{"password": MaskedSecret, "host": "new"},
		map[string]string{"password": "hunter2", "host": "old"})
	assert.Equal(t, map[string]string{"password": "hunter2", "host": "new"}, got)
}

func TestMaskConfig_HidesSecrets(t *testing.T) {
	in := map[string]string{"token": "t", "password": "p", "host": "h", "username": ""}
	got := MaskConfig("ntfy", in)
	assert.Equal(t, MaskedSecret, got["token"])
	assert.Equal(t, MaskedSecret, got["password"])
	assert.Equal(t, "h", got["host"])
	assert.Equal(t, "t", in["token"], "input must not be mutated")
}

func TestValidateServiceConfig_SMTPRequiresHostFromTo(t *testing.T) {
	assert.Error(t, ValidateServiceConfig("smtp", map[string]string{"from": "a@b.c", "to": "d@e.f"}))
	assert.Error(t, ValidateServiceConfig("smtp", map[string]string{"host": "h", "to": "d@e.f"}))
	assert.Error(t, ValidateServiceConfig("smtp", map[string]string{"host": "h", "from": "a@b.c"}))
	assert.NoError(t, ValidateServiceConfig("smtp", sampleConfigs["smtp"]))
	assert.Error(t, ValidateServiceConfig("nope", nil))
}

func TestShoutrrrSender_ErrorsRedactSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	secretPath := "SuperSecretToken123"
	cfg := map[string]string{"url": srv.URL + "/" + secretPath}

	err := ShoutrrrSender{}.Send(context.Background(), "webhook", cfg, Message{Title: "t", Text: "hi"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secretPath)
	assert.NotContains(t, err.Error(), cfg["url"])

	// Failing before the network (bad URL) must not leak either.
	err = ShoutrrrSender{}.Send(context.Background(), "advanced", map[string]string{"url": "bogus://user:pa55w0rdXYZ@host/tok3nXYZ"}, Message{Text: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "pa55w0rdXYZ")
	assert.NotContains(t, err.Error(), "tok3nXYZ")
}

func TestShoutrrrSender_RespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := ShoutrrrSender{}.Send(ctx, "webhook", map[string]string{"url": srv.URL + "/h"}, Message{Text: "hi"})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.False(t, strings.Contains(err.Error(), srv.URL+"/h"))
}

func TestRedactError_StripsSecretsAndURL(t *testing.T) {
	for typ, cfg := range sampleConfigs {
		u, err := BuildURL(typ, cfg)
		require.NoError(t, err)
		raw := errors.New("failed: Post \"" + u + "\": dial tcp: refused")
		got := redactError(raw, typ, cfg, u).Error()
		assert.NotContains(t, got, u, typ)
		for _, k := range SecretFields(typ) {
			assert.NotContains(t, got, cfg[k], typ+"/"+k)
		}
	}
}

func mustURL(t *testing.T, typ string, cfg map[string]string) *url.URL {
	t.Helper()
	s, err := BuildURL(typ, cfg)
	require.NoError(t, err)
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

// Shoutrrr uses fromaddress/toaddresses verbatim for MAIL FROM/RCPT TO, so
// "Name <addr>" must be split: the address there, the sender name in fromname.
func TestBuildURL_SMTPDisplayNames(t *testing.T) {
	cfg := map[string]string{
		"host": "email-smtp.us-west-2.amazonaws.com", "port": "587", "encryption": "starttls",
		"from": "Engager Docksmith <docksmith@mail.rooday.com>",
		"to":   "Rudhra Raveendran <contact@roo.day>, ops@example.com",
	}
	raw, err := BuildURL("smtp", cfg)
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "docksmith@mail.rooday.com", q.Get("fromaddress"))
	assert.Equal(t, "Engager Docksmith", q.Get("fromname"))
	assert.Equal(t, "contact@roo.day,ops@example.com", q.Get("toaddresses"))
	assert.NotContains(t, u.RawQuery, "+", "spaces must be escaped as %20; shoutrrr does not decode +")
	_, err = shoutrrr.CreateSender(raw)
	require.NoError(t, err)

	assert.NoError(t, ValidateServiceConfig("smtp", cfg))
	assert.Error(t, ValidateServiceConfig("smtp", map[string]string{"host": "h", "from": "not an address", "to": "a@example.com"}))
	_, err = BuildURL("smtp", map[string]string{"host": "h", "from": "not an address", "to": "a@example.com"})
	assert.Error(t, err)
	_, err = BuildURL("smtp", map[string]string{"host": "h", "from": "a@example.com", "to": "x, <broken"})
	assert.Error(t, err)
}

func TestBuildURL_FieldMapping(t *testing.T) {
	po := mustURL(t, "pushover", sampleConfigs["pushover"])
	pw, _ := po.User.Password()
	assert.Equal(t, sampleConfigs["pushover"]["user_key"], po.Host)
	assert.Equal(t, sampleConfigs["pushover"]["token"], pw)

	dc := mustURL(t, "discord", sampleConfigs["discord"])
	assert.Equal(t, "123456789", dc.Host)
	assert.Equal(t, "AbCdEf-gh_ij", dc.User.Username())

	tg := mustURL(t, "telegram", sampleConfigs["telegram"])
	tpw, _ := tg.User.Password()
	assert.Equal(t, "123456", tg.User.Username())
	assert.Equal(t, "ABC-DEF1234ghIkl", tpw)
	assert.Equal(t, "@chan1,12345", tg.Query().Get("chats"))

	gf := mustURL(t, "gotify", sampleConfigs["gotify"])
	assert.Equal(t, "gotify.example.com:8080", gf.Host)
	assert.Equal(t, "/AbCdEfGhIjKlMnO", gf.Path)
}

func TestBuildURL_DiscordRejectsUnsafeParts(t *testing.T) {
	for _, w := range []string{
		"https://discord.com/api/webhooks/12@3/token",
		"https://discord.com/api/webhooks/abc/token",
		"https://discord.com/api/webhooks/123/to%40ken",
		"https://discord.com/api/webhooks/123/to%3Aken",
	} {
		_, err := BuildURL("discord", map[string]string{"webhook_url": w})
		assert.Error(t, err, w)
	}
}

func TestShoutrrrSender_RedactsAllQueryValues(t *testing.T) {
	cases := map[string]func() (string, func()){
		"server 500": func() (string, func()) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			}))
			return srv.URL, srv.Close
		},
		"unreachable": func() (string, func()) {
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			u := srv.URL
			srv.Close()
			return u, func() {}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			base, done := mk()
			defer done()
			cfg := map[string]string{"url": base + "/hook?token=BBBsecretTok&key=AAAsecretKey&z=a+b%2Fc"}
			err := ShoutrrrSender{}.Send(context.Background(), "webhook", cfg, Message{Title: "t", Text: "hi"})
			require.Error(t, err)
			msg := err.Error()
			for _, secret := range []string{"AAAsecretKey", "BBBsecretTok", "a+b%2Fc", "a b/c", "a%2Bb%2Fc"} {
				assert.NotContains(t, msg, secret, msg)
			}
		})
	}
}
