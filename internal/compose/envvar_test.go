package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainsEnvVar(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"nginx:latest", false},
		{"ghcr.io/user/app:v1.0", false},
		{"${IMAGE}", true},
		{"${IMAGE:-nginx:latest}", true},
		{"${REGISTRY}/app:${TAG:-latest}", true},
	}
	for _, tt := range tests {
		if got := ContainsEnvVar(tt.input); got != tt.want {
			t.Errorf("ContainsEnvVar(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		envVars map[string]string
		want    string
	}{
		{
			name:  "no env vars",
			input: "nginx:latest",
			want:  "nginx:latest",
		},
		{
			name:  "default value with colon-dash",
			input: "${OPENCLAW_IMAGE:-openclaw:latest}",
			want:  "openclaw:latest",
		},
		{
			name:  "default value with dash only",
			input: "${IMAGE-nginx:latest}",
			want:  "nginx:latest",
		},
		{
			name:    "env var set overrides default",
			input:   "${MY_IMAGE:-fallback:v1}",
			envVars: map[string]string{"MY_IMAGE": "custom:v2"},
			want:    "custom:v2",
		},
		{
			name:    "empty env var uses default with colon-dash",
			input:   "${MY_IMAGE:-fallback:v1}",
			envVars: map[string]string{"MY_IMAGE": ""},
			want:    "fallback:v1",
		},
		{
			name:  "unresolvable simple var left as-is",
			input: "${UNKNOWN_VAR}",
			want:  "${UNKNOWN_VAR}",
		},
		{
			name:    "simple var resolved from env",
			input:   "${MY_IMAGE}",
			envVars: map[string]string{"MY_IMAGE": "myapp:v3"},
			want:    "myapp:v3",
		},
		{
			name:  "mixed resolved and literal",
			input: "${REGISTRY:-docker.io}/myapp:${TAG:-latest}",
			want:  "docker.io/myapp:latest",
		},
		{
			name:    "mixed with env override",
			input:   "${REGISTRY:-docker.io}/myapp:${TAG:-latest}",
			envVars: map[string]string{"REGISTRY": "ghcr.io"},
			want:    "ghcr.io/myapp:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars for this test
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}
			// Clear any env vars from previous tests that might interfere
			if tt.envVars == nil {
				os.Unsetenv("OPENCLAW_IMAGE")
				os.Unsetenv("MY_IMAGE")
				os.Unsetenv("REGISTRY")
				os.Unsetenv("TAG")
				os.Unsetenv("UNKNOWN_VAR")
				os.Unsetenv("IMAGE")
			}

			got := ResolveEnvVars(tt.input)
			if got != tt.want {
				t.Errorf("ResolveEnvVars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReplaceTagInEnvVar(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		newTag string
		want   string
		wantOk bool
	}{
		{
			name:   "colon-dash default with tag",
			input:  "${OPENCLAW_IMAGE:-openclaw:latest}",
			newTag: "v2.0",
			want:   "${OPENCLAW_IMAGE:-openclaw:v2.0}",
			wantOk: true,
		},
		{
			name:   "dash default with tag",
			input:  "${IMAGE-nginx:1.25}",
			newTag: "1.26",
			want:   "${IMAGE-nginx:1.26}",
			wantOk: true,
		},
		{
			name:   "default without tag appends one",
			input:  "${IMAGE:-nginx}",
			newTag: "1.26",
			want:   "${IMAGE:-nginx:1.26}",
			wantOk: true,
		},
		{
			name:   "no env var returns original",
			input:  "nginx:latest",
			newTag: "1.26",
			want:   "nginx:latest",
			wantOk: false,
		},
		{
			name:   "simple var without default returns original",
			input:  "${IMAGE}",
			newTag: "v2",
			want:   "${IMAGE}",
			wantOk: false,
		},
		{
			name:   "registry with port in default",
			input:  "${IMAGE:-registry.example.com:5000/myapp:v1}",
			newTag: "v2",
			want:   "${IMAGE:-registry.example.com:5000/myapp:v2}",
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ReplaceTagInEnvVar(tt.input, tt.newTag)
			if ok != tt.wantOk {
				t.Errorf("ReplaceTagInEnvVar(%q, %q) ok = %v, want %v", tt.input, tt.newTag, ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("ReplaceTagInEnvVar(%q, %q) = %q, want %q", tt.input, tt.newTag, got, tt.want)
			}
		})
	}
}

func TestIsFullImageRef(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"ghcr.io/openclaw/openclaw:latest", true},
		{"nginx:1.25", true},
		{"registry.example.com:5000/myapp:v1", true},
		{"v1.2.3", false},
		{"latest", false},
		{"sha-abc123", false},
	}
	for _, tt := range tests {
		if got := IsFullImageRef(tt.value); got != tt.want {
			t.Errorf("IsFullImageRef(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestReplaceTagInValue(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		newTag string
		want   string
	}{
		{
			name:   "full image ref with registry",
			value:  "ghcr.io/openclaw/openclaw:latest",
			newTag: "v2.0",
			want:   "ghcr.io/openclaw/openclaw:v2.0",
		},
		{
			name:   "full image ref without tag",
			value:  "ghcr.io/openclaw/openclaw",
			newTag: "v2.0",
			want:   "ghcr.io/openclaw/openclaw:v2.0",
		},
		{
			name:   "image with port in registry",
			value:  "registry.example.com:5000/myapp:v1",
			newTag: "v2",
			want:   "registry.example.com:5000/myapp:v2",
		},
		{
			name:   "simple image:tag",
			value:  "nginx:1.25",
			newTag: "1.26",
			want:   "nginx:1.26",
		},
		{
			name:   "bare tag",
			value:  "v1.2.3",
			newTag: "v1.3.0",
			want:   "v1.3.0",
		},
		{
			name:   "bare latest",
			value:  "latest",
			newTag: "v2.0",
			want:   "v2.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceTagInValue(tt.value, tt.newTag)
			if got != tt.want {
				t.Errorf("ReplaceTagInValue(%q, %q) = %q, want %q", tt.value, tt.newTag, got, tt.want)
			}
		})
	}
}

func TestUpdateDotEnvVar(t *testing.T) {
	tests := []struct {
		name        string
		envContent  string
		varName     string
		newTag      string
		wantContent string
		wantErr     bool
	}{
		{
			name:        "full image ref with registry",
			envContent:  "OPENCLAW_IMAGE=ghcr.io/openclaw/openclaw:latest\n",
			varName:     "OPENCLAW_IMAGE",
			newTag:      "v2.0",
			wantContent: "OPENCLAW_IMAGE=ghcr.io/openclaw/openclaw:v2.0\n",
		},
		{
			name:        "bare tag value",
			envContent:  "APP_VERSION=v1.2.3\n",
			varName:     "APP_VERSION",
			newTag:      "v1.3.0",
			wantContent: "APP_VERSION=v1.3.0\n",
		},
		{
			name:        "preserves comments and other vars",
			envContent:  "# This is a comment\nOTHER_VAR=hello\nMY_IMAGE=ghcr.io/user/app:v1.0\nANOTHER=world\n",
			varName:     "MY_IMAGE",
			newTag:      "v2.0",
			wantContent: "# This is a comment\nOTHER_VAR=hello\nMY_IMAGE=ghcr.io/user/app:v2.0\nANOTHER=world\n",
		},
		{
			name:        "preserves blank lines",
			envContent:  "FOO=bar\n\nMY_IMAGE=nginx:1.25\n\nBAZ=qux\n",
			varName:     "MY_IMAGE",
			newTag:      "1.26",
			wantContent: "FOO=bar\n\nMY_IMAGE=nginx:1.26\n\nBAZ=qux\n",
		},
		{
			name:        "double quoted value",
			envContent:  "MY_IMAGE=\"ghcr.io/user/app:v1.0\"\n",
			varName:     "MY_IMAGE",
			newTag:      "v3.0",
			wantContent: "MY_IMAGE=\"ghcr.io/user/app:v3.0\"\n",
		},
		{
			name:        "single quoted value",
			envContent:  "MY_IMAGE='ghcr.io/user/app:v1.0'\n",
			varName:     "MY_IMAGE",
			newTag:      "v3.0",
			wantContent: "MY_IMAGE='ghcr.io/user/app:v3.0'\n",
		},
		{
			name:    "variable not found",
			envContent: "OTHER=value\n",
			varName: "MISSING",
			newTag:  "v1.0",
			wantErr: true,
		},
		{
			name:        "registry with port",
			envContent:  "MY_IMAGE=registry.example.com:5000/myapp:v1\n",
			varName:     "MY_IMAGE",
			newTag:      "v2",
			wantContent: "MY_IMAGE=registry.example.com:5000/myapp:v2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			envPath := filepath.Join(dir, ".env")
			if err := os.WriteFile(envPath, []byte(tt.envContent), 0644); err != nil {
				t.Fatalf("failed to write .env: %v", err)
			}

			err := UpdateDotEnvVar(dir, tt.varName, tt.newTag)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := os.ReadFile(envPath)
			if err != nil {
				t.Fatalf("failed to read .env: %v", err)
			}
			if string(got) != tt.wantContent {
				t.Errorf("got:\n%s\nwant:\n%s", string(got), tt.wantContent)
			}
		})
	}
}
