// Shared constants for progress tracking UI

export interface StageInfo {
  icon: string;
  label: string;
  description: string;
}

// Stage display information for update/rollback operations
export const STAGE_INFO: Record<string, StageInfo> = {
  'validating': {
    icon: 'fa-magnifying-glass',
    label: 'Validating',
    description: 'Checking container configuration...'
  },
  'backup': {
    icon: 'fa-floppy-disk',
    label: 'Backup',
    description: 'Creating backup of current state...'
  },
  'updating_compose': {
    icon: 'fa-file-pen',
    label: 'Updating Compose',
    description: 'Modifying compose file with new version...'
  },
  'pulling_image': {
    icon: 'fa-cloud-arrow-down',
    label: 'Pulling Image',
    description: 'Downloading container image...'
  },
  'recreating': {
    icon: 'fa-rotate',
    label: 'Recreating',
    description: 'Recreating container with new image...'
  },
  'health_check': {
    icon: 'fa-heart-pulse',
    label: 'Health Check',
    description: 'Waiting for container to become healthy...'
  },
  'rolling_back': {
    icon: 'fa-rotate-left',
    label: 'Rolling Back',
    description: 'Reverting to previous version...'
  },
  'complete': {
    icon: 'fa-circle-check',
    label: 'Complete',
    description: 'Operation completed successfully'
  },
  'failed': {
    icon: 'fa-circle-xmark',
    label: 'Failed',
    description: 'Operation failed'
  },
};

// Restart-specific stages (for ContainerPage)
export const RESTART_STAGES: Record<string, StageInfo> = {
  'saving': {
    icon: 'fa-floppy-disk',
    label: 'Saving',
    description: 'Saving settings to compose file...'
  },
  'stopping': {
    icon: 'fa-circle-stop',
    label: 'Stopping',
    description: 'Stopping the container gracefully...'
  },
  'starting': {
    icon: 'fa-circle-play',
    label: 'Starting',
    description: 'Starting the container...'
  },
  'checking': {
    icon: 'fa-heart-pulse',
    label: 'Health Check',
    description: 'Verifying container is running correctly...'
  },
  'dependents': {
    icon: 'fa-link',
    label: 'Processing Dependents',
    description: 'Restarting dependent containers...'
  },
  'restarting_dependents': {
    icon: 'fa-rotate',
    label: 'Restarting Dependents',
    description: 'Restarting dependent containers...'
  },
  'complete': {
    icon: 'fa-circle-check',
    label: 'Complete',
    description: 'Restart completed successfully'
  },
  'failed': {
    icon: 'fa-circle-xmark',
    label: 'Failed',
    description: 'Restart failed'
  },
};

// Common log entry type
export interface LogEntry {
  time: number;
  message: string;
  type: 'info' | 'success' | 'error' | 'warning' | 'stage';
  icon?: string;
}

// Helper to get stage icon class
export function getStageIconClass(stage: string, stageInfo = STAGE_INFO): string {
  return stageInfo[stage]?.icon || 'fa-hourglass-half';
}

// Helper to get stage label
export function getStageLabel(stage: string, stageInfo = STAGE_INFO): string {
  return stageInfo[stage]?.label || stage;
}

// Helper to get stage description
export function getStageDescription(stage: string, stageInfo = STAGE_INFO): string {
  return stageInfo[stage]?.description || '';
}
