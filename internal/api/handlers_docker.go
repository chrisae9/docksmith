package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/chis/docksmith/internal/output"
)

// DockerConfig represents the structure of docker config.json
type DockerConfig struct {
	Auths map[string]any `json:"auths"`
}

// DockerRegistryInfo contains information about configured registries
type DockerRegistryInfo struct {
	Registries []string `json:"registries"`
	ConfigPath string   `json:"config_path"`
}

// handleDockerConfig returns information about configured Docker registries
func (s *Server) handleDockerConfig(w http.ResponseWriter, r *http.Request) {
	// Determine the config path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/home/docksmith"
	}
	configPath := homeDir + "/.docker/config.json"

	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		output.WriteJSONError(w, fmt.Errorf("failed to read Docker config: %w", err))
		return
	}

	// Parse the config
	var config DockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		output.WriteJSONError(w, fmt.Errorf("failed to parse Docker config: %w", err))
		return
	}

	// Extract registry names
	registries := make([]string, 0, len(config.Auths))
	for registry := range config.Auths {
		// Clean up registry names for better display
		cleanName := registry
		if strings.HasPrefix(registry, "https://index.docker.io/v1/") {
			if registry == "https://index.docker.io/v1/" {
				cleanName = "Docker Hub (index.docker.io)"
			} else {
				// Skip token-related entries
				continue
			}
		}
		registries = append(registries, cleanName)
	}

	info := DockerRegistryInfo{
		Registries: registries,
		ConfigPath: configPath,
	}

	output.WriteJSONData(w, info)
}
