package version

import "testing"

func TestExtractFromImage(t *testing.T) {
	extractor := NewExtractor()

	tests := []struct {
		name             string
		imageStr         string
		expectRegistry   string
		expectRepository string
		expectVersioned  bool
		expectMajor      int
	}{
		{
			name:             "simple image with tag",
			imageStr:         "nginx:1.21.3",
			expectRegistry:   "docker.io",
			expectRepository: "library/nginx",
			expectVersioned:  true,
			expectMajor:      1,
		},
		{
			name:             "ghcr.io image",
			imageStr:         "ghcr.io/linuxserver/plex:1.32.0",
			expectRegistry:   "ghcr.io",
			expectRepository: "linuxserver/plex",
			expectVersioned:  true,
			expectMajor:      1,
		},
		{
			name:             "user/repo format",
			imageStr:         "myuser/myapp:2.0.0",
			expectRegistry:   "docker.io",
			expectRepository: "myuser/myapp",
			expectVersioned:  true,
			expectMajor:      2,
		},
		{
			name:             "latest tag",
			imageStr:         "nginx:latest",
			expectRegistry:   "docker.io",
			expectRepository: "library/nginx",
			expectVersioned:  false,
		},
		{
			name:             "no tag specified",
			imageStr:         "nginx",
			expectRegistry:   "docker.io",
			expectRepository: "library/nginx",
			expectVersioned:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := extractor.ExtractFromImage(tt.imageStr)

			if info.Registry != tt.expectRegistry {
				t.Errorf("Registry: got %q, want %q", info.Registry, tt.expectRegistry)
			}

			if info.Repository != tt.expectRepository {
				t.Errorf("Repository: got %q, want %q", info.Repository, tt.expectRepository)
			}

			if info.Tag.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned: got %v, want %v", info.Tag.IsVersioned, tt.expectVersioned)
			}

			if tt.expectVersioned {
				if info.Tag.Version == nil {
					t.Fatal("Expected version but got nil")
				}
				if info.Tag.Version.Major != tt.expectMajor {
					t.Errorf("Major: got %d, want %d", info.Tag.Version.Major, tt.expectMajor)
				}
			}
		})
	}
}

func TestCompareImages(t *testing.T) {
	extractor := NewExtractor()

	tests := []struct {
		name     string
		current  string
		new      string
		expected ChangeType
	}{
		{
			name:     "patch upgrade",
			current:  "nginx:1.21.3",
			new:      "nginx:1.21.4",
			expected: PatchChange,
		},
		{
			name:     "minor upgrade",
			current:  "nginx:1.21.3",
			new:      "nginx:1.22.0",
			expected: MinorChange,
		},
		{
			name:     "major upgrade",
			current:  "nginx:1.21.3",
			new:      "nginx:2.0.0",
			expected: MajorChange,
		},
		{
			name:     "downgrade",
			current:  "nginx:2.0.0",
			new:      "nginx:1.21.3",
			expected: Downgrade,
		},
		{
			name:     "no change",
			current:  "nginx:1.21.3",
			new:      "nginx:1.21.3",
			expected: NoChange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.CompareImages(tt.current, tt.new)
			if result != tt.expected {
				t.Errorf("CompareImages(%q, %q) = %v, want %v",
					tt.current, tt.new, result, tt.expected)
			}
		})
	}
}
