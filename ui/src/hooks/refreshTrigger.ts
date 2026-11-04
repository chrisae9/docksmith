// Decision logic for the background-check trigger that backs the dashboard
// refresh. Kept free of React so the deduplicated-trigger behaviour is testable.

import type { TriggerCheckResponse } from '../types/api';

export type PollMode = 'triggered' | 'joined';

export type RefreshPlan =
  /** Wait for the background check before reading cached data. */
  | { poll: true; mode: PollMode }
  /** No check was started and none is running: cached data is already current. */
  | { poll: false };

/**
 * Decides whether a refresh needs to wait for a background check.
 *
 * `/api/trigger-check` deduplicates requests to one per minute, so a 200 does
 * not imply a check started. Only a started check, or one that is already
 * running, is worth waiting for; otherwise the cached result is used at once.
 */
export function planRefresh(trigger: TriggerCheckResponse | null | undefined): RefreshPlan {
  if (!trigger) return { poll: false };
  if (trigger.triggered) return { poll: true, mode: 'triggered' };
  if (trigger.checking) return { poll: true, mode: 'joined' };
  return { poll: false };
}

/**
 * Reports whether the background check a refresh is waiting on has finished.
 *
 * A check this client started is finished once it stops running and its last
 * run advances past the trigger time. A check this client merely joined began
 * before the trigger time, so waiting for the timestamp to advance would spin
 * until the poll deadline; it is finished as soon as it stops running.
 */
export function checkFinished(
  mode: PollMode,
  triggerAt: number,
  status: { checking?: boolean; last_background_run?: string },
): boolean {
  if (status.checking) return false;
  if (mode === 'joined') return true;
  const lastRunMs = status.last_background_run ? Date.parse(status.last_background_run) : 0;
  return lastRunMs >= triggerAt;
}
