package graph

import (
	"fmt"
)

// TopologicalSort returns nodes in dependency order using Kahn's algorithm.
// Nodes with no dependencies come first, then nodes that depend on them, etc.
// Returns an error if the graph contains cycles.
func (g *Graph) TopologicalSort() ([]string, error) {
	// Calculate in-degree (number of dependencies) for each node
	inDegree := make(map[string]int)

	// Initialize in-degree for all nodes
	for id := range g.Nodes {
		inDegree[id] = 0
	}

	// Count dependencies for each node (only count dependencies that exist in graph)
	for _, node := range g.Nodes {
		for _, depID := range node.Dependencies {
			// Only count this dependency if the node actually exists in the graph
			if _, exists := g.Nodes[depID]; exists {
				inDegree[node.ID]++
			}
		}
	}

	// Queue for nodes with no dependencies
	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Result list
	sorted := []string{}

	// Process nodes in order
	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]

		sorted = append(sorted, current)

		// Process all nodes that depend on current
		for _, dependent := range g.GetDependents(current) {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// If we haven't processed all nodes, there's a cycle
	if len(sorted) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return sorted, nil
}

// GetUpdateOrder returns the order in which containers should be updated.
// This is the same as topological sort - dependencies first.
func (g *Graph) GetUpdateOrder() ([]string, error) {
	return g.TopologicalSort()
}

// GetRestartOrder returns the order in which containers should be restarted
// after an update. This is the REVERSE of update order - dependents first.
func (g *Graph) GetRestartOrder() ([]string, error) {
	updateOrder, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Reverse the order
	restartOrder := make([]string, len(updateOrder))
	for i, j := 0, len(updateOrder)-1; i < len(updateOrder); i, j = i+1, j-1 {
		restartOrder[i] = updateOrder[j]
	}

	return restartOrder, nil
}
