import { describe, expect, it } from 'vitest';
import { buildPinPlan, pinPlanToUpdates, revalidatePinPlan } from './pinPlan';
import type { UnifiedContainerItem } from '../types/api';

function container(
  overrides: Partial<UnifiedContainerItem> & { name: string },
): UnifiedContainerItem {
  return {
    id: overrides.name,
    image: 'ghcr.io/acme/app:latest',
    state: 'running',
    health_status: 'healthy',
    created: 0,
    networks: [],
    has_update_data: true,
    update_status: 'UP_TO_DATE_PINNABLE',
    current_tag: 'latest',
    recommended_tag: '1.0.0',
    ...overrides,
  };
}

describe('buildPinPlan', () => {
  it('captures the target tag for each selected pinnable container', () => {
    const app = container({ name: 'app' });
    const web = container({ name: 'web', recommended_tag: '2.1.0' });

    const plan = buildPinPlan([app, web], new Set(['app', 'web']));

    expect(plan?.entries).toEqual([
      expect.objectContaining({ name: 'app', currentTag: 'latest', target: '1.0.0' }),
      expect.objectContaining({ name: 'web', currentTag: 'latest', target: '2.1.0' }),
    ]);
  });

  it('ignores selected containers that are not pinnable', () => {
    const blocked = container({ name: 'app', update_status: 'UPDATE_AVAILABLE_BLOCKED' });
    const plan = buildPinPlan([blocked], new Set(['app']));
    expect(plan).toBeNull();
  });

  it('captures nothing for unselected containers', () => {
    const app = container({ name: 'app' });
    expect(buildPinPlan([app], new Set(['other']))).toBeNull();
  });

  it('never carries a force flag', () => {
    const plan = buildPinPlan([container({ name: 'app' })], new Set(['app']));
    expect(plan?.entries[0]).not.toHaveProperty('force');
    expect(pinPlanToUpdates(plan!)[0]).not.toHaveProperty('force');
  });
});

describe('revalidatePinPlan', () => {
  it('keeps an unchanged plan and its original tag', () => {
    const app = container({ name: 'app' });
    const plan = buildPinPlan([app], new Set(['app']))!;

    // A background refresh that changes nothing the user confirmed.
    const refreshed = [container({ name: 'app' })];
    expect(revalidatePinPlan(plan, refreshed)).toBe(plan);
    expect(pinPlanToUpdates(revalidatePinPlan(plan, refreshed)!).at(0)?.target_version).toBe('1.0.0');
  });

  it('invalidates the plan when a pinned container becomes a blocked update', () => {
    // Regression: the dialog used to stay open and its Pin button fell through
    // to the bulk update, which sent force=true for the new blocked target.
    const plan = buildPinPlan([container({ name: 'app' })], new Set(['app']))!;
    const afterRefresh = [
      container({
        name: 'app',
        update_status: 'UPDATE_AVAILABLE_BLOCKED',
        recommended_tag: '2.0.0',
      }),
    ];

    expect(revalidatePinPlan(plan, afterRefresh)).toBeNull();
  });

  it('invalidates the plan when the target tag changes', () => {
    const plan = buildPinPlan([container({ name: 'app' })], new Set(['app']))!;
    const afterRefresh = [container({ name: 'app', recommended_tag: '1.0.1' })];
    expect(revalidatePinPlan(plan, afterRefresh)).toBeNull();
  });

  it('invalidates the plan when a container disappears or loses update data', () => {
    const plan = buildPinPlan(
      [container({ name: 'app' }), container({ name: 'web', recommended_tag: '2.1.0' })],
      new Set(['app', 'web']),
    )!;

    expect(revalidatePinPlan(plan, [container({ name: 'app' })])).toBeNull();
    expect(
      revalidatePinPlan(plan, [
        container({ name: 'app' }),
        container({ name: 'web', recommended_tag: '2.1.0', has_update_data: false }),
      ]),
    ).toBeNull();
  });
});

describe('pinPlanToUpdates', () => {
  it('emits the confirmed target and never a force flag', () => {
    const plan = buildPinPlan(
      [container({ name: 'app', stack: 'core' })],
      new Set(['app']),
    )!;

    expect(pinPlanToUpdates(plan)).toEqual([
      {
        name: 'app',
        target_version: '1.0.0',
        stack: 'core',
        change_type: 0,
        old_resolved_version: '',
        new_resolved_version: '',
      },
    ]);
  });
});
