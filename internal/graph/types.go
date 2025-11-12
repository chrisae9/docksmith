package graph

// Node represents a container in the dependency graph.
type Node struct {
	// ID is the unique identifier for this node (container ID or name)
	ID string

	// Dependencies are the nodes that this node depends on.
	// For example, if torrent depends on VPN, VPN is in torrent's Dependencies.
	Dependencies []string

	// Metadata stores additional information about the container
	Metadata map[string]string
}

// Graph represents a dependency graph of containers.
type Graph struct {
	// Nodes maps node IDs to their Node structures
	Nodes map[string]*Node

	// Adjacency stores the adjacency list representation
	// Key: node ID, Value: list of nodes that depend on this node
	Adjacency map[string][]string
}

// NewGraph creates a new empty dependency graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:     make(map[string]*Node),
		Adjacency: make(map[string][]string),
	}
}

// AddNode adds a node to the graph.
func (g *Graph) AddNode(node *Node) {
	g.Nodes[node.ID] = node

	// Initialize adjacency list if not exists
	if _, exists := g.Adjacency[node.ID]; !exists {
		g.Adjacency[node.ID] = []string{}
	}

	// Add edges for dependencies
	for _, depID := range node.Dependencies {
		// Ensure dependency node exists in adjacency map
		if _, exists := g.Adjacency[depID]; !exists {
			g.Adjacency[depID] = []string{}
		}
		// Add edge: depID -> node.ID (depID has node.ID depending on it)
		g.Adjacency[depID] = append(g.Adjacency[depID], node.ID)
	}
}

// GetNode retrieves a node by ID.
func (g *Graph) GetNode(id string) (*Node, bool) {
	node, exists := g.Nodes[id]
	return node, exists
}

// GetDependents returns all nodes that depend on the given node.
func (g *Graph) GetDependents(id string) []string {
	if dependents, exists := g.Adjacency[id]; exists {
		return dependents
	}
	return []string{}
}
