import { describe, expect, it } from 'vitest';
import { checkFinished, planRefresh } from './refreshTrigger';

describe('planRefresh', () => {
  it('does not poll when a deduplicated trigger started no check', () => {
    // Regression: every 200 used to be treated as a started check, so the UI
    // polled /api/status for the full 15s deadline waiting for a last-run
    // timestamp that could not advance.
    expect(
      planRefresh({ triggered: false, checking: false, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toEqual({ poll: false });
  });

  it('polls when the trigger started a check', () => {
    expect(planRefresh({ triggered: true, checking: true, last_background_run: '' })).toEqual({
      poll: true,
      mode: 'triggered',
    });
  });

  it('polls when joining a check that is already running', () => {
    expect(
      planRefresh({ triggered: false, checking: true, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toEqual({ poll: true, mode: 'joined' });
  });

  it('does not poll without a trigger response', () => {
    expect(planRefresh(null)).toEqual({ poll: false });
    expect(planRefresh(undefined)).toEqual({ poll: false });
  });
});

describe('checkFinished', () => {
  const triggerAt = Date.parse('2026-11-03T20:00:10Z');

  it('a started check is finished only when the run advances past the trigger', () => {
    expect(
      checkFinished('triggered', triggerAt, { checking: true, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toBe(false);
    expect(
      checkFinished('triggered', triggerAt, { checking: false, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toBe(false);
    expect(
      checkFinished('triggered', triggerAt, { checking: false, last_background_run: '2026-11-03T20:00:12Z' }),
    ).toBe(true);
  });

  it('a joined check is finished as soon as it stops running', () => {
    // Its last run predates the trigger, so requiring the timestamp to advance
    // would spin until the poll deadline.
    expect(
      checkFinished('joined', triggerAt, { checking: true, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toBe(false);
    expect(
      checkFinished('joined', triggerAt, { checking: false, last_background_run: '2026-11-03T20:00:00Z' }),
    ).toBe(true);
  });
});
