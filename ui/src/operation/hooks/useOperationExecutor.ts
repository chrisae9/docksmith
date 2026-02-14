import { useEffect, useRef } from 'react';
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom';
import type { OperationInfo, OperationAction, ContainerState } from '../types';
import { getExecutor } from '../executors/registry';

interface UseOperationExecutorOptions {
  operationInfo: OperationInfo | null;
  dispatch: React.Dispatch<OperationAction>;
  runId: string;
}

export function useOperationExecutor({ operationInfo, dispatch, runId }: UseOperationExecutorOptions) {
  const hasStarted = useRef(false);
  const navigate = useNavigate();
  const location = useLocation();
  const [, setSearchParams] = useSearchParams();

  useEffect(() => {
    if (hasStarted.current || !operationInfo) return;
    hasStarted.current = true;

    // Clear location state to prevent re-triggering on page refresh
    navigate(location.pathname, { replace: true, state: null });

    // Initialize containers based on operation type
    const containers = buildInitialContainers(operationInfo);

    // Dispatch INIT action
    dispatch({
      type: 'INIT',
      runId,
      operationType: operationInfo.type,
      operationInfo,
      containers,
    });

    // Get and run the executor
    const executor = getExecutor(operationInfo.type);
    executor.execute(operationInfo, { dispatch, runId, setSearchParams });
  }, [operationInfo]);
}

// Build initial container state based on operation type
function buildInitialContainers(info: OperationInfo): ContainerState[] {
  switch (info.type) {
    case 'restart':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, badge: info.force ? 'Force' : undefined }];
    case 'update':
      return info.containers.map(c => ({ name: c.name, status: 'pending' as const, percent: 0, message: 'Queued', badge: c.force ? 'Force' : undefined, versionFrom: c.old_resolved_version, versionTo: c.new_resolved_version || c.target_version }));
    case 'rollback':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, badge: 'Rollback', versionFrom: info.newVersion, versionTo: info.oldVersion }];
    case 'start':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, message: 'Starting container...' }];
    case 'stop':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, message: 'Stopping container...' }];
    case 'remove':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, message: 'Removing container...' }];
    case 'stackRestart':
      return info.containers.map(name => ({ name, status: 'pending' as const, percent: 0, badge: info.force ? 'Force' : undefined }));
    case 'stackStop':
      return info.containers.map(name => ({ name, status: 'pending' as const, percent: 0 }));
    case 'fixMismatch':
      return [{ name: info.containerName, status: 'in_progress', percent: 0, message: 'Syncing container to compose file...' }];
    case 'batchFixMismatch':
      return info.containerNames.map(name => ({ name, status: 'pending' as const, percent: 0, message: 'Waiting...' }));
    case 'mixed':
      return [
        ...info.updates.map(u => ({ name: u.name, status: 'pending' as const, percent: 0, message: 'Update pending' })),
        ...info.mismatches.map(name => ({ name, status: 'pending' as const, percent: 0, message: 'Fix pending' })),
      ];
    case 'batchLabel':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Applying label changes...' }));
    case 'batchStart':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Starting...' }));
    case 'batchStop':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Stopping...' }));
    case 'batchRestart':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Restarting...' }));
    case 'batchRemove':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Removing...' }));
    case 'labelRollback':
      return info.containers.map(name => ({ name, status: 'in_progress' as const, percent: 0, message: 'Rolling back label changes...' }));
    default:
      return [];
  }
}
