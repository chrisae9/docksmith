package main

import "os"

const (
	// DefaultDBPath is the default location for the docksmith database
	DefaultDBPath = "/data/docksmith.db"
)

// getDBPath returns the database path from environment variable or default
func getDBPath() string {
	if path := os.Getenv("DB_PATH"); path != "" {
		return path
	}
	return DefaultDBPath
}
