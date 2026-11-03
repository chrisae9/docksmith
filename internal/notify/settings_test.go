package notify

import (
	"context"
	"testing"
)

type memCfg map[string]string

func (m memCfg) GetConfig(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}
func (m memCfg) SetConfig(_ context.Context, k, v string) error { m[k] = v; return nil }

func TestLoadSettings_Defaults(t *testing.T) {
	s, err := LoadSettings(context.Background(), memCfg{}, "host1")
	if err != nil {
		t.Fatal(err)
	}
	if s.MinLevel != "patch" || s.SummaryCron != "0 8 * * *" || s.ServerName != "host1" {
		t.Errorf("bad defaults: %+v", s)
	}
	if s.Timing[EventDigestChanged] != TimingOff {
		t.Errorf("digest_changed = %s, want off", s.Timing[EventDigestChanged])
	}
	for _, k := range []EventKind{EventUpdateFailed, EventRollback, EventUpdateBlocked, EventCheckFailing} {
		if s.Timing[k] != TimingImmediate {
			t.Errorf("%s = %s, want immediate", k, s.Timing[k])
		}
	}
	for _, k := range []EventKind{EventNewVersion, EventUpdateSucceeded, EventComposeMismatch} {
		if s.Timing[k] != TimingSummary {
			t.Errorf("%s = %s, want summary", k, s.Timing[k])
		}
	}
}

func TestSettings_RoundTripFillsMissingTiming(t *testing.T) {
	cfg := memCfg{}
	s := DefaultSettings("h")
	s.MinLevel = "major"
	s.Timing = map[EventKind]Timing{EventNewVersion: TimingImmediate}
	if err := SaveSettings(context.Background(), cfg, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(context.Background(), cfg, "h")
	if err != nil {
		t.Fatal(err)
	}
	if got.MinLevel != "major" || got.Timing[EventNewVersion] != TimingImmediate || got.Timing[EventRollback] != TimingImmediate || got.Timing[EventDigestChanged] != TimingOff {
		t.Errorf("bad merge: %+v", got)
	}
}

func TestLoadSettings_PartialBlobUsesDefaults(t *testing.T) {
	cfg := memCfg{"notify_settings": `{"base_url":"http://x"}`}
	got, err := LoadSettings(context.Background(), cfg, "h")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://x" || got.MinLevel != "patch" || got.SummaryCron != "0 8 * * *" || got.ServerName != "h" || got.Timing[EventRollback] != TimingImmediate {
		t.Errorf("bad: %+v", got)
	}
}
