package compose

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// LoadComposeFile loads a docker-compose.yaml file while preserving comments and formatting.
// Returns a ComposeFile struct with parsed YAML nodes.
func LoadComposeFile(path string) (*ComposeFile, error) {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	// Parse YAML while preserving structure
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Find the services node
	var servicesNode *yaml.Node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		// Root is typically a document node containing a mapping node
		mappingNode := root.Content[0]
		if mappingNode.Kind == yaml.MappingNode {
			// Find "services" key
			for i := 0; i < len(mappingNode.Content); i += 2 {
				keyNode := mappingNode.Content[i]
				if keyNode.Value == "services" {
					servicesNode = mappingNode.Content[i+1]
					break
				}
			}
		}
	}

	if servicesNode == nil {
		return nil, fmt.Errorf("no services section found in compose file")
	}

	return &ComposeFile{
		Path:     path,
		Root:     &root,
		Services: servicesNode,
	}, nil
}

// FindServiceByContainerName finds a service by its container_name label.
// Returns the service if found, or an error if not found.
func (cf *ComposeFile) FindServiceByContainerName(containerName string) (*Service, error) {
	if cf.Services == nil || cf.Services.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("invalid services structure")
	}

	// Iterate through services (key-value pairs)
	for i := 0; i < len(cf.Services.Content); i += 2 {
		serviceNameNode := cf.Services.Content[i]
		serviceDefNode := cf.Services.Content[i+1]

		// Check if this service has a container_name that matches
		containerNameValue := getContainerName(serviceDefNode)

		// Match by container_name if present, otherwise by service name
		if containerNameValue == containerName || serviceNameNode.Value == containerName {
			return &Service{
				Name: serviceNameNode.Value,
				Node: serviceDefNode,
			}, nil
		}
	}

	return nil, fmt.Errorf("service not found for container: %s", containerName)
}

// getContainerName extracts the container_name value from a service definition node.
func getContainerName(serviceNode *yaml.Node) string {
	if serviceNode.Kind != yaml.MappingNode {
		return ""
	}

	for i := 0; i < len(serviceNode.Content); i += 2 {
		keyNode := serviceNode.Content[i]
		valueNode := serviceNode.Content[i+1]

		if keyNode.Value == "container_name" {
			return valueNode.Value
		}
	}

	return ""
}

// GetOrCreateLabelsNode retrieves or creates the labels node for a service.
// Returns the labels node (either existing or newly created).
func (s *Service) GetOrCreateLabelsNode() (*yaml.Node, error) {
	if s.Node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("service node is not a mapping")
	}

	// Look for existing labels node
	for i := 0; i < len(s.Node.Content); i += 2 {
		keyNode := s.Node.Content[i]
		if keyNode.Value == "labels" {
			s.Labels = s.Node.Content[i+1]
			return s.Labels, nil
		}
	}

	// Labels node doesn't exist, create it
	labelsKeyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "labels",
	}

	labelsValueNode := &yaml.Node{
		Kind:    yaml.SequenceNode,
		Content: []*yaml.Node{},
	}

	// Add labels to service node
	s.Node.Content = append(s.Node.Content, labelsKeyNode, labelsValueNode)
	s.Labels = labelsValueNode

	return s.Labels, nil
}

// SetLabel adds or updates a label in the service's labels section.
// Handles both sequence (array) and mapping (object) style labels.
func (s *Service) SetLabel(key, value string) error {
	labelsNode, err := s.GetOrCreateLabelsNode()
	if err != nil {
		return err
	}

	labelString := fmt.Sprintf("%s=%s", key, value)

	if labelsNode.Kind == yaml.SequenceNode {
		// Array-style labels: ["key=value", "key2=value2"]
		// Check if label already exists and update it
		for _, node := range labelsNode.Content {
			if node.Kind == yaml.ScalarNode {
				// Parse existing label
				existingKey := parseLabelKey(node.Value)
				if existingKey == key {
					// Update existing label
					node.Value = labelString
					return nil
				}
			}
		}

		// Label doesn't exist, add it
		newLabelNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: labelString,
		}
		labelsNode.Content = append(labelsNode.Content, newLabelNode)
		return nil

	} else if labelsNode.Kind == yaml.MappingNode {
		// Object-style labels: {key: value, key2: value2}
		// Check if label already exists and update it
		for i := 0; i < len(labelsNode.Content); i += 2 {
			keyNode := labelsNode.Content[i]
			if keyNode.Value == key {
				// Update existing label value
				labelsNode.Content[i+1].Value = value
				return nil
			}
		}

		// Label doesn't exist, add it
		keyNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: key,
		}
		valueNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: value,
		}
		labelsNode.Content = append(labelsNode.Content, keyNode, valueNode)
		return nil
	}

	return fmt.Errorf("unsupported labels format: %v", labelsNode.Kind)
}

// RemoveLabel removes a label from the service's labels section.
func (s *Service) RemoveLabel(key string) error {
	labelsNode, err := s.GetOrCreateLabelsNode()
	if err != nil {
		return err
	}

	if labelsNode.Kind == yaml.SequenceNode {
		// Array-style labels
		newContent := []*yaml.Node{}
		removed := false

		for _, node := range labelsNode.Content {
			if node.Kind == yaml.ScalarNode {
				existingKey := parseLabelKey(node.Value)
				if existingKey != key {
					newContent = append(newContent, node)
				} else {
					removed = true
				}
			}
		}

		if !removed {
			// Label doesn't exist - that's fine, it's already "removed"
			return nil
		}

		labelsNode.Content = newContent

		// If labels section is now empty, remove the labels key entirely
		if len(newContent) == 0 {
			s.removeLabelsKey()
		}

		return nil

	} else if labelsNode.Kind == yaml.MappingNode {
		// Object-style labels
		newContent := []*yaml.Node{}
		removed := false

		for i := 0; i < len(labelsNode.Content); i += 2 {
			keyNode := labelsNode.Content[i]
			valueNode := labelsNode.Content[i+1]

			if keyNode.Value != key {
				newContent = append(newContent, keyNode, valueNode)
			} else {
				removed = true
			}
		}

		if !removed {
			// Label doesn't exist - that's fine, it's already "removed"
			return nil
		}

		labelsNode.Content = newContent

		// If labels section is now empty, remove the labels key entirely
		if len(newContent) == 0 {
			s.removeLabelsKey()
		}

		return nil
	}

	return fmt.Errorf("unsupported labels format")
}

// removeLabelsKey removes the labels key from the service definition entirely
func (s *Service) removeLabelsKey() {
	if s.Node.Kind != yaml.MappingNode {
		return
	}

	newContent := []*yaml.Node{}
	for i := 0; i < len(s.Node.Content); i += 2 {
		keyNode := s.Node.Content[i]
		valueNode := s.Node.Content[i+1]

		if keyNode.Value != "labels" {
			newContent = append(newContent, keyNode, valueNode)
		}
	}

	s.Node.Content = newContent
}

// parseLabelKey extracts the key from a "key=value" label string.
func parseLabelKey(label string) string {
	for i, c := range label {
		if c == '=' {
			return label[:i]
		}
	}
	return label
}

// parseLabelValue extracts the value from a "key=value" label string.
func parseLabelValue(label string) string {
	for i, c := range label {
		if c == '=' {
			return label[i+1:]
		}
	}
	return ""
}

// GetAllLabels returns all labels from the service as a map.
// Handles both sequence (array) and mapping (object) style labels.
func (s *Service) GetAllLabels() (map[string]string, error) {
	labels := make(map[string]string)

	// First try to find the labels node if not already set
	if s.Labels == nil {
		_, _ = s.GetOrCreateLabelsNode()
	}

	if s.Labels == nil {
		return labels, nil
	}

	if s.Labels.Kind == yaml.SequenceNode {
		// Array-style labels: ["key=value", "key2=value2"]
		for _, node := range s.Labels.Content {
			if node.Kind == yaml.ScalarNode {
				key := parseLabelKey(node.Value)
				value := parseLabelValue(node.Value)
				labels[key] = value
			}
		}
	} else if s.Labels.Kind == yaml.MappingNode {
		// Object-style labels: {key: value, key2: value2}
		for i := 0; i < len(s.Labels.Content); i += 2 {
			keyNode := s.Labels.Content[i]
			valueNode := s.Labels.Content[i+1]
			labels[keyNode.Value] = valueNode.Value
		}
	}

	return labels, nil
}

// Save saves the compose file with preserved formatting and comments.
// Overwrites the original file.
func (cf *ComposeFile) Save() error {
	// Encode YAML
	var buf []byte
	encoder := yaml.NewEncoder(&writeBuffer{data: &buf})
	encoder.SetIndent(2)

	if err := encoder.Encode(cf.Root); err != nil {
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close encoder: %w", err)
	}

	// Write to file
	if err := os.WriteFile(cf.Path, buf, 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

// writeBuffer implements io.Writer for yaml.Encoder.
type writeBuffer struct {
	data *[]byte
}

func (wb *writeBuffer) Write(p []byte) (n int, err error) {
	*wb.data = append(*wb.data, p...)
	return len(p), nil
}

// BackupComposeFile creates a timestamped backup of a compose file.
// Returns the path to the backup file.
func BackupComposeFile(composePath string) (string, error) {
	// Read original file
	data, err := os.ReadFile(composePath)
	if err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	// Generate backup filename with timestamp
	dir := filepath.Dir(composePath)
	base := filepath.Base(composePath)
	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(dir, fmt.Sprintf(".%s.backup.%s", base, timestamp))

	// Write backup
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}

	return backupPath, nil
}

// RestoreFromBackup restores a compose file from a backup.
func RestoreFromBackup(composePath, backupPath string) error {
	// Read backup
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Write to compose file
	if err := os.WriteFile(composePath, data, 0644); err != nil {
		return fmt.Errorf("failed to restore compose file: %w", err)
	}

	return nil
}

// GetIncludePaths extracts the list of included files from a compose file.
// Returns nil if there are no includes.
func GetIncludePaths(composePath string) ([]string, error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, nil
	}

	mappingNode := root.Content[0]
	if mappingNode.Kind != yaml.MappingNode {
		return nil, nil
	}

	// Look for "include" key
	for i := 0; i < len(mappingNode.Content); i += 2 {
		keyNode := mappingNode.Content[i]
		if keyNode.Value == "include" {
			includeNode := mappingNode.Content[i+1]

			// Include can be a sequence (array) of file paths
			if includeNode.Kind == yaml.SequenceNode {
				var includes []string
				for _, item := range includeNode.Content {
					if item.Kind == yaml.ScalarNode {
						// Resolve relative paths
						includePath := item.Value
						if !filepath.IsAbs(includePath) {
							baseDir := filepath.Dir(composePath)
							includePath = filepath.Join(baseDir, includePath)
						}
						includes = append(includes, includePath)
					}
				}
				return includes, nil
			}
		}
	}

	return nil, nil
}

// FindServiceInIncludes searches through included compose files to find which file contains a service.
// Returns the path to the file containing the service, or an error if not found.
func FindServiceInIncludes(composePath, containerName string) (string, error) {
	includes, err := GetIncludePaths(composePath)
	if err != nil {
		return "", err
	}

	if len(includes) == 0 {
		return "", nil // No includes, use main file
	}

	// Search each included file
	for _, includePath := range includes {
		// Try to load the included file
		cf, err := LoadComposeFile(includePath)
		if err != nil {
			// Skip files that can't be loaded (might not have services section)
			continue
		}

		// Check if this file contains the service
		_, err = cf.FindServiceByContainerName(containerName)
		if err == nil {
			// Found it!
			return includePath, nil
		}
	}

	return "", fmt.Errorf("service not found in any included files: %s", containerName)
}

// LoadComposeFileOrIncluded loads a compose file, automatically finding the correct included file
// if the main compose file uses includes.
func LoadComposeFileOrIncluded(composePath, containerName string) (*ComposeFile, error) {
	// First, try to load the compose file normally
	cf, err := LoadComposeFile(composePath)
	if err == nil {
		return cf, nil
	}

	// If we got an error about no services section, check if this file uses includes
	if err != nil && err.Error() == "no services section found in compose file" {
		// Try to find the service in included files
		includedFile, findErr := FindServiceInIncludes(composePath, containerName)
		if findErr != nil {
			return nil, fmt.Errorf("main file has no services and service not found in includes: %w", findErr)
		}

		if includedFile == "" {
			return nil, err // No includes found, return original error
		}

		// Load the included file that contains the service
		return LoadComposeFile(includedFile)
	}

	// Some other error, return it
	return nil, err
}

// Ensure writeBuffer implements io.Writer
var _ io.Writer = (*writeBuffer)(nil)
