package main

import (
	"fmt"
	"os"

	"github.com/chis/docksmith/internal/storage"
)

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

// InitializeStorage initializes and returns a storage service using the configured database path
func InitializeStorage() (*storage.SQLiteStorage, error) {
	storageService, err := storage.NewSQLiteStorage(getDBPath())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	return storageService, nil
}
