package graph

// HasCycles checks if the graph contains any circular dependencies.
// Uses depth-first search with color marking.
func (g *Graph) HasCycles() bool {
	// Color states: 0 = white (unvisited), 1 = gray (visiting), 2 = black (visited)
	color := make(map[string]int)

	// Initialize all nodes as white
	for id := range g.Nodes {
		color[id] = 0
	}

	// Check each unvisited node
	for id := range g.Nodes {
		if color[id] == 0 {
			if g.hasCycleDFS(id, color) {
				return true
			}
		}
	}

	return false
}

// hasCycleDFS performs depth-first search to detect cycles.
// Uses a recursion stack to track the current path.
func (g *Graph) hasCycleDFS(nodeID string, color map[string]int) bool {
	// Mark as visiting (gray)
	color[nodeID] = 1

	node, exists := g.GetNode(nodeID)
	if !exists {
		color[nodeID] = 2
		return false
	}

	// Visit all dependencies
	for _, depID := range node.Dependencies {
		// If dependency is gray (in current path), we found a cycle
		if color[depID] == 1 {
			return true
		}

		// If dependency is white (unvisited), recursively check
		if color[depID] == 0 {
			if g.hasCycleDFS(depID, color) {
				return true
			}
		}
		// If black (2), already fully processed, skip
	}

	// Mark as visited (black)
	color[nodeID] = 2
	return false
}

// FindCycle attempts to find a cycle in the graph and returns the cycle path.
// Returns nil if no cycle exists.
func (g *Graph) FindCycle() []string {
	color := make(map[string]int)
	parent := make(map[string]string)
	var cyclePath []string

	for id := range g.Nodes {
		color[id] = 0
	}

	for id := range g.Nodes {
		if color[id] == 0 {
			if path := g.findCycleDFS(id, color, parent); path != nil {
				cyclePath = path
				break
			}
		}
	}

	return cyclePath
}

// findCycleDFS performs DFS and constructs the cycle path if found.
func (g *Graph) findCycleDFS(nodeID string, color map[string]int, parent map[string]string) []string {
	color[nodeID] = 1

	node, exists := g.GetNode(nodeID)
	if !exists {
		return nil
	}

	for _, depID := range node.Dependencies {
		if color[depID] == 1 {
			// Found cycle - reconstruct path
			path := []string{depID}
			current := nodeID
			for current != depID && current != "" {
				path = append([]string{current}, path...)
				nextParent, exists := parent[current]
				if !exists {
					break
				}
				current = nextParent
			}
			return path
		}

		if color[depID] == 0 {
			parent[depID] = nodeID
			if cycle := g.findCycleDFS(depID, color, parent); cycle != nil {
				return cycle
			}
		}
	}

	color[nodeID] = 2
	return nil
}
