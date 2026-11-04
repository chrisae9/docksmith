import type { UnifiedContainerItem } from '../types/api';
import { isPinnable } from './status';

/**
 * A pin confirmation captures the exact containers and target tags the user was
 * shown. Container data refreshes in the background while the dialog is open, so
 * re-running the bulk update at confirm time would act on whatever the latest
 * status says — including a container that has since become a blocked update,
 * which the bulk handler would turn into a forced update.
 *
 * A plan therefore carries no force flag at all: pinning never skips pre-update
 * checks, whatever the container's status becomes after the dialog opened.
 */
export interface PinPlanEntry {
  name: string;
  currentTag: string;
  target: string;
  stack: string;
  change_type: number;
  old_resolved_version: string;
  new_resolved_version: string;
}

export interface PinPlan {
  entries: PinPlanEntry[];
}

function pinTarget(container: UnifiedContainerItem): string {
  return container.recommended_tag || container.latest_version || '';
}

function toEntry(container: UnifiedContainerItem): PinPlanEntry {
  return {
    name: container.name,
    currentTag: container.current_tag || '?',
    target: pinTarget(container),
    stack: container.stack || '',
    change_type: container.change_type || 0,
    old_resolved_version: container.current_version || '',
    new_resolved_version: container.latest_resolved_version || container.latest_version || '',
  };
}

/**
 * Captures the pin targets for the selected containers, or null when none of the
 * selected containers is pinnable.
 */
export function buildPinPlan(
  containers: UnifiedContainerItem[],
  selected: ReadonlySet<string>,
): PinPlan | null {
  const entries = containers
    .filter(c => selected.has(c.name) && c.has_update_data && isPinnable(c.update_status || ''))
    .map(toEntry);
  return entries.length > 0 ? { entries } : null;
}

/**
 * Returns the plan unchanged when every entry still matches the live container
 * data, or null when anything the user confirmed has changed — the container is
 * no longer pinnable, its target tag moved, or it disappeared from the list.
 */
export function revalidatePinPlan(
  plan: PinPlan,
  containers: UnifiedContainerItem[],
): PinPlan | null {
  const live = new Map(containers.map(c => [c.name, c]));
  for (const entry of plan.entries) {
    const container = live.get(entry.name);
    if (!container || !container.has_update_data) return null;
    if (!isPinnable(container.update_status || '')) return null;
    if (pinTarget(container) !== entry.target) return null;
  }
  return plan;
}

/** Builds the `/operation` update payload from a validated plan. Never sets force. */
export function pinPlanToUpdates(plan: PinPlan) {
  return plan.entries.map(entry => ({
    name: entry.name,
    target_version: entry.target,
    stack: entry.stack,
    change_type: entry.change_type,
    old_resolved_version: entry.old_resolved_version,
    new_resolved_version: entry.new_resolved_version,
  }));
}
