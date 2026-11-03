package notify

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chis/docksmith/internal/storage"
)

// Timing says when an event kind is delivered to every enabled service.
type Timing string

const (
	TimingOff       Timing = "off"
	TimingImmediate Timing = "immediate"
	TimingSummary   Timing = "summary"
)

// ConfigStore is the key/value store used to persist settings.
type ConfigStore interface {
	GetConfig(ctx context.Context, key string) (string, bool, error)
	SetConfig(ctx context.Context, key, value string) error
}

// Settings are the global notification settings.
type Settings struct {
	ServerName  string               `json:"server_name"`
	BaseURL     string               `json:"base_url"`
	MinLevel    string               `json:"min_level"`
	SummaryCron string               `json:"summary_cron"`
	Timing      map[EventKind]Timing `json:"timing"`
}

func defaultTiming() map[EventKind]Timing {
	return map[EventKind]Timing{
		EventUpdateFailed:    TimingImmediate,
		EventRollback:        TimingImmediate,
		EventUpdateBlocked:   TimingImmediate,
		EventCheckFailing:    TimingImmediate,
		EventNewVersion:      TimingSummary,
		EventDigestChanged:   TimingOff, // opt-in, globally or per container via docksmith.notify-digest
		EventUpdateSucceeded: TimingSummary,
		EventComposeMismatch: TimingSummary,
	}
}

// DefaultSettings returns the default settings for the given hostname.
func DefaultSettings(hostname string) Settings {
	return Settings{
		ServerName:  hostname,
		MinLevel:    "patch",
		SummaryCron: "0 8 * * *",
		Timing:      defaultTiming(),
	}
}

// LoadSettings reads settings; a missing key yields defaults and missing timing entries are filled from defaults.
func LoadSettings(ctx context.Context, cfg ConfigStore, hostname string) (Settings, error) {
	raw, ok, err := cfg.GetConfig(ctx, storage.NotifySettingsKey)
	if err != nil {
		return Settings{}, fmt.Errorf("load notify settings: %w", err)
	}
	s := DefaultSettings(hostname)
	if !ok {
		return s, nil
	}
	stored := s
	stored.Timing = nil
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return Settings{}, fmt.Errorf("parse notify settings: %w", err)
	}
	for k, v := range s.Timing {
		if _, has := stored.Timing[k]; !has {
			if stored.Timing == nil {
				stored.Timing = map[EventKind]Timing{}
			}
			stored.Timing[k] = v
		}
	}
	return stored, nil
}

// SaveSettings persists settings.
func SaveSettings(ctx context.Context, cfg ConfigStore, s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return cfg.SetConfig(ctx, storage.NotifySettingsKey, string(b))
}
