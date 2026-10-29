package registry

import (
	"encoding/base64"
	"testing"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestParseGHCRTokenFromDockerConfig(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"ghcr entry", `{"auths":{"ghcr.io":{"auth":"` + b64("user:ghcr-token") + `"}}}`, "ghcr-token"},
		{"lscr entry", `{"auths":{"lscr.io":{"auth":"` + b64("user:lscr-token") + `"}}}`, "lscr-token"},
		{"ghcr wins when both", `{"auths":{"ghcr.io":{"auth":"` + b64("u:ghcr") + `"},"lscr.io":{"auth":"` + b64("u:lscr") + `"}}}`, "ghcr"},
		{"no ghcr keys", `{"auths":{"https://index.docker.io/v1/":{"auth":"` + b64("u:hub") + `"}}}`, ""},
		{"malformed base64", `{"auths":{"ghcr.io":{"auth":"!!!"}}}`, ""},
		{"missing colon", `{"auths":{"ghcr.io":{"auth":"` + b64("tokenonly") + `"}}}`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseGHCRTokenFromDockerConfig([]byte(tt.config)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
