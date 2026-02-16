import type { OperationState } from './types';

export function selectSuccessCount(state: OperationState): number {
  return state.containers.filter(c => c.status === 'success').length;
}

export function selectFailedCount(state: OperationState): number {
  return state.containers.filter(c => c.status === 'failed').length;
}

export function selectIsTerminal(state: OperationState): boolean {
  return state.phase === 'completed' || state.phase === 'failed' || state.phase === 'partial';
}

export function selectHasErrors(state: OperationState): boolean {
  return selectFailedCount(state) > 0 || state.phase === 'failed';
}

export function selectDisplayPercent(state: OperationState): number {
  if (state.containers.length > 1) {
    // Use completed count — honest progress tied to the X/N counter
    const done = state.containers.filter(c => c.status === 'success' || c.status === 'failed').length;
    return Math.round((done / state.containers.length) * 100);
  }
  return state.currentPercent;
}

