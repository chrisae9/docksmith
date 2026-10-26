package notify

import (
	"context"
	"log"
	"time"
)

// schedulerTick is how often the scheduler checks whether a summary is due.
var schedulerTick = time.Minute

// Scheduler flushes pending summaries when the SummaryCron schedule fires.
type Scheduler struct {
	settings func() Settings
	current  CurrentVersions
	flush    func(ctx context.Context, current CurrentVersions, now time.Time)

	// Owned by the scheduler goroutine.
	cron string
	next time.Time
}

// NewScheduler creates a scheduler that flushes d's summaries.
func NewScheduler(d *Dispatcher, settings func() Settings, current CurrentVersions) *Scheduler {
	return &Scheduler{settings: settings, current: current, flush: d.FlushSummaries}
}

// Start runs the scheduler in a goroutine until ctx is done. It does not block.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		s.tick(ctx, time.Now())
		t := time.NewTicker(schedulerTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.tick(ctx, time.Now())
			}
		}
	}()
}

// tick recomputes the next fire time when the cron changed, otherwise flushes
// once now has reached it. A missed fire (e.g. after sleep) fires once, late.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	cron := s.settings().SummaryCron
	if cron != s.cron {
		s.cron = cron
		s.next = s.nextFire(now, true)
		return
	}
	if s.next.IsZero() || now.Before(s.next) {
		return
	}
	s.flush(ctx, s.current, now)
	s.next = s.nextFire(now, false)
}

func (s *Scheduler) nextFire(now time.Time, logErr bool) time.Time {
	fires, err := NextFires(s.cron, time.Local, now, 1)
	if err != nil || len(fires) == 0 {
		if logErr {
			log.Printf("notify: summary schedule %q is unusable; summaries paused: %v", s.cron, err)
		}
		return time.Time{}
	}
	return fires[0]
}
