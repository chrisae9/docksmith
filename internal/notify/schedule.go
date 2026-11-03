package notify

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ParseSchedule parses a standard 5-field cron expression.
// Descriptors (@daily, @every) and CRON_TZ=/TZ= prefixes are rejected; the
// schedule always runs in the local timezone.
func ParseSchedule(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "@") || strings.HasPrefix(expr, "CRON_TZ=") || strings.HasPrefix(expr, "TZ=") {
		return nil, fmt.Errorf("only plain 5-field cron expressions are supported: %q", expr)
	}
	if len(strings.Fields(expr)) != 5 {
		return nil, fmt.Errorf("expected 5 fields in cron expression: %q", expr)
	}
	return cron.ParseStandard(expr)
}

// ValidateSchedule checks that expr parses, fires at all, and never fires
// closer together than minGap within the next 366 days (or 10,000 fires).
func ValidateSchedule(expr string, minGap time.Duration, loc *time.Location, now time.Time) error {
	sched, err := ParseSchedule(expr)
	if err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	end := now.AddDate(0, 0, 366)
	prev := sched.Next(now)
	if prev.IsZero() || prev.After(end) {
		return fmt.Errorf("schedule %q never fires", expr)
	}
	for i := 0; i < 10000; i++ {
		next := sched.Next(prev)
		if next.IsZero() || next.After(end) {
			return nil
		}
		if next.Sub(prev) < minGap {
			return fmt.Errorf("schedule %q fires too often: %s between runs (minimum %s)", expr, next.Sub(prev), minGap)
		}
		prev = next
	}
	return nil
}

// NextFires returns the next n fire times of expr after now, in loc.
func NextFires(expr string, loc *time.Location, now time.Time, n int) ([]time.Time, error) {
	sched, err := ParseSchedule(expr)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.Local
	}
	out := make([]time.Time, 0, n)
	t := now.In(loc)
	for len(out) < n {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// TimezoneInfo returns the local timezone name and a warning when TZ is set
// but time.Local resolved to UTC (the zone name was likely not found).
func TimezoneInfo() (name string, warning string) {
	name = time.Local.String()
	if tz := os.Getenv("TZ"); tz != "" && tz != "UTC" && tz != "Etc/UTC" && name == "UTC" {
		warning = fmt.Sprintf("TZ=%q is set but resolved to UTC; check the timezone name", tz)
	}
	return name, warning
}
