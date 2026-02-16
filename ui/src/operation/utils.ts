import type {
  OperationInfo,
  OperationType,
  OperationError,
  RestartOperation,
  BatchLabelOperation,
} from './types';

// Parse location state into OperationInfo (from router navigation)
export function parseLocationState(state: any): OperationInfo | null {
  if (!state) return null;

  if (state.restart) {
    return { type: 'restart', ...state.restart };
  }
  if (state.update) {
    return { type: 'update', ...state.update };
  }
  if (state.rollback) {
    return { type: 'rollback', ...state.rollback };
  }
  if (state.start) {
    return { type: 'start', ...state.start };
  }
  if (state.stop) {
    return { type: 'stop', ...state.stop };
  }
  if (state.remove) {
    return { type: 'remove', ...state.remove };
  }
  if (state.stackRestart) {
    return { type: 'stackRestart', ...state.stackRestart };
  }
  if (state.stackStop) {
    return { type: 'stackStop', ...state.stackStop };
  }
  if (state.fixMismatch) {
    return { type: 'fixMismatch', ...state.fixMismatch };
  }
  if (state.batchFixMismatch) {
    return { type: 'batchFixMismatch', ...state.batchFixMismatch };
  }
  if (state.mixed) {
    return { type: 'mixed', ...state.mixed };
  }
  if (state.batchLabel) {
    return { type: 'batchLabel', ...state.batchLabel };
  }
  if (state.batchStart) {
    return { type: 'batchStart', ...state.batchStart };
  }
  if (state.batchStop) {
    return { type: 'batchStop', ...state.batchStop };
  }
  if (state.batchRestart) {
    return { type: 'batchRestart', ...state.batchRestart };
  }
  if (state.batchRemove) {
    return { type: 'batchRemove', ...state.batchRemove };
  }
  if (state.labelRollback) {
    return { type: 'labelRollback', ...state.labelRollback };
  }
  return null;
}

// Centralized error classification (replaces scattered isPreCheckFailure)
export function classifyError(errorMessage: string): OperationError {
  const raw = errorMessage;

  // Pre-check failures
  if (
    errorMessage.includes('pre-update check failed') ||
    errorMessage.includes('failed pre-update check') ||
    errorMessage.includes('script exited with code') ||
    errorMessage.includes('pre-check failed') ||
    errorMessage.includes('Pre-update check') ||
    errorMessage.includes('pre_update_check') ||
    errorMessage.includes('Dependent container')
  ) {
    return {
      kind: 'precheck',
      raw,
      message: 'Pre-update check failed',
      retryable: true,
      forceable: true,
    };
  }

  // Transport/network errors
  if (
    errorMessage.includes('network') ||
    errorMessage.includes('ECONNREFUSED') ||
    errorMessage.includes('fetch failed') ||
    errorMessage.includes('Failed to fetch')
  ) {
    return {
      kind: 'transport',
      raw,
      message: 'Network error',
      retryable: true,
      forceable: false,
    };
  }

  // Timeout errors
  if (
    errorMessage.includes('timeout') ||
    errorMessage.includes('timed out') ||
    errorMessage.includes('Timed out')
  ) {
    return {
      kind: 'timeout',
      raw,
      message: 'Operation timed out',
      retryable: true,
      forceable: false,
    };
  }

  // Recovery errors
  if (
    errorMessage.includes('recover') ||
    errorMessage.includes('Recovery')
  ) {
    return {
      kind: 'recovery',
      raw,
      message: errorMessage,
      retryable: false,
      forceable: false,
    };
  }

  // Generic backend error
  return {
    kind: 'backend',
    raw,
    message: errorMessage,
    retryable: false,
    forceable: false,
  };
}

// Convenience: check if error is a pre-check failure (backwards-compatible helper)
export function isPreCheckFailure(errorMessage: string): boolean {
  return classifyError(errorMessage).kind === 'precheck';
}

// Page title for each operation type
export function getPageTitle(operationType: OperationType | null, recoveredOperation?: any): string {
  // Handle recovery mode
  if (recoveredOperation && !operationType) {
    const opType = recoveredOperation.operation_type;
    switch (opType) {
      case 'restart': return 'Restarting Container';
      case 'single': return 'Updating Container';
      case 'batch': return 'Updating Containers';
      case 'rollback': return 'Rolling Back';
      case 'start': return 'Starting Container';
      case 'stop': return 'Stopping Container';
      case 'remove': return 'Removing Container';
      case 'label_change': return 'Applying Labels';
      case 'fix_mismatch': return 'Fixing Compose Mismatch';
      case 'batch_start': return 'Starting Containers';
      case 'batch_stop': return 'Stopping Containers';
      case 'batch_restart': return 'Restarting Containers';
      case 'batch_remove': return 'Removing Containers';
      default: return 'Operation Progress';
    }
  }
  switch (operationType) {
    case 'restart': return 'Restarting Container';
    case 'update': return 'Updating Containers';
    case 'rollback': return 'Rolling Back';
    case 'start': return 'Starting Container';
    case 'stop': return 'Stopping Container';
    case 'remove': return 'Removing Container';
    case 'stackRestart': return 'Restarting Stack';
    case 'stackStop': return 'Stopping Stack';
    case 'fixMismatch': return 'Fixing Compose Mismatch';
    case 'batchFixMismatch': return 'Fixing Compose Mismatches';
    case 'mixed': return 'Processing Containers';
    case 'batchLabel': return 'Applying Labels';
    case 'batchStart': return 'Starting Containers';
    case 'batchStop': return 'Stopping Containers';
    case 'batchRestart': return 'Restarting Containers';
    case 'batchRemove': return 'Removing Containers';
    case 'labelRollback': return 'Rolling Back Labels';
    default: return 'Progress';
  }
}

// Describe label changes for display (used in restart with saveSettings)
export function describeChanges(changes: RestartOperation['labelChanges']): string[] {
  if (!changes) return [];
  const descriptions: string[] = [];

  if (changes.ignore !== undefined) {
    descriptions.push(changes.ignore ? 'Enable ignore' : 'Disable ignore');
  }
  if (changes.allow_latest !== undefined) {
    descriptions.push(changes.allow_latest ? 'Allow :latest tag' : 'Disallow :latest tag');
  }
  if (changes.version_pin_major !== undefined || changes.version_pin_minor !== undefined || changes.version_pin_patch !== undefined) {
    if (changes.version_pin_major) {
      descriptions.push('Pin to major version');
    } else if (changes.version_pin_minor) {
      descriptions.push('Pin to minor version');
    } else if (changes.version_pin_patch) {
      descriptions.push('Pin to patch version');
    } else {
      descriptions.push('Remove version pin');
    }
  }
  if (changes.tag_regex !== undefined) {
    descriptions.push(changes.tag_regex ? `Set tag filter: ${changes.tag_regex}` : 'Remove tag filter');
  }
  if (changes.script !== undefined) {
    descriptions.push(changes.script ? `Set script: ${changes.script.split('/').pop()}` : 'Remove pre-update script');
  }
  if (changes.restart_after !== undefined) {
    descriptions.push(changes.restart_after ? `Set restart deps: ${changes.restart_after}` : 'Remove restart dependencies');
  }

  return descriptions;
}

// Describe a batch label operation for display
export function describeLabelOp(labelOp: BatchLabelOperation['labelOp']): string {
  if (labelOp.ignore === true) return 'Ignore';
  if (labelOp.ignore === false) return 'Unignore';
  if (labelOp.allow_latest === true) return 'Allow :latest';
  if (labelOp.allow_latest === false) return 'Disallow :latest';
  if (labelOp.version_pin_major) return 'Pin Major';
  if (labelOp.version_pin_minor) return 'Pin Minor';
  if (labelOp.version_pin_patch) return 'Pin Patch';
  if (labelOp.version_pin_major === false && labelOp.version_pin_minor === false && labelOp.version_pin_patch === false) return 'Unpin';
  if (labelOp.tag_regex === '') return 'Clear Tag Filter';
  if (labelOp.tag_regex) return `Set Tag Filter: ${labelOp.tag_regex}`;
  if (labelOp.script === '') return 'Clear Script';
  if (labelOp.script) return `Set Script: ${labelOp.script}`;
  return 'Apply Labels';
}

// Format operation type for display (used in NoOperationFallback)
export function formatOpType(type: string): string {
  switch (type) {
    case 'single': return 'Update';
    case 'batch': return 'Batch Update';
    case 'restart': return 'Restart';
    case 'start': return 'Start';
    case 'stop': return 'Stop';
    case 'remove': return 'Remove';
    case 'rollback': return 'Rollback';
    case 'label_change': return 'Label Change';
    case 'fix_mismatch': return 'Fix Mismatch';
    case 'batch_start': return 'Batch Start';
    case 'batch_stop': return 'Batch Stop';
    case 'batch_restart': return 'Batch Restart';
    case 'batch_remove': return 'Batch Remove';
    default: return type;
  }
}

// Get FontAwesome icon class for operation status
export function getStatusIcon(status: string): string {
  switch (status) {
    case 'in_progress': case 'pending_restart': return 'fa-spinner fa-spin';
    case 'complete': return 'fa-circle-check';
    case 'failed': return 'fa-circle-xmark';
    default: return 'fa-circle-question';
  }
}

// Get CSS class for operation status
export function getStatusClass(status: string): string {
  switch (status) {
    case 'in_progress': case 'pending_restart': return 'status-in-progress';
    case 'complete': return 'status-success';
    case 'failed': return 'status-failed';
    default: return '';
  }
}

// Force button label based on operation type
export function getForceButtonLabel(operationType: OperationType | null): string {
  switch (operationType) {
    case 'restart': return 'Force Restart';
    case 'rollback': return 'Force Rollback';
    case 'stackRestart': return 'Force Restart Stack';
    default: return 'Force Retry';
  }
}
