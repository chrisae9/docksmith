package compose

import "gopkg.in/yaml.v3"

// ComposeFile represents a parsed docker-compose.yaml file.
// Uses yaml.v3.Node to preserve comments, formatting, and structure.
type ComposeFile struct {
	// Path is the absolute path to the compose file
	Path string

	// Root is the root YAML node containing the entire document
	Root *yaml.Node

	// Services is a reference to the services node for quick access
	Services *yaml.Node
}

// Service represents a service definition within a compose file.
// Provides access to the service's YAML node for manipulation.
type Service struct {
	// Name is the service name (key in the services map)
	Name string

	// Node is the YAML node containing the service definition
	Node *yaml.Node

	// Labels is a reference to the labels node if it exists
	Labels *yaml.Node
}

// Label represents a single label key-value pair.
type Label struct {
	Key   string
	Value string
}
