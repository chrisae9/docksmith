import { useCallback } from 'react';
import { getContainerStatus, getContainerLabels } from '../api/client';
import { useAsyncResource } from './useAsyncResource';

// Shared by the container configuration sub-pages.
export function useContainerData(containerName: string | undefined) {
  const load = useCallback(async () => {
    const [status, labels] = await Promise.all([
      getContainerStatus(), getContainerLabels(containerName!),
    ]);
    if (!status.success || !status.data) throw new Error(status.error || 'Failed to load container status');
    const container = status.data.containers.find(item => item.container_name === containerName);
    if (!container) throw new Error(`Container "${containerName}" not found`);
    return labels.success && labels.data ? { ...container, labels: labels.data.labels || {} } : container;
  }, [containerName]);
  const { data: container, loading, error } = useAsyncResource(containerName || '', load);
  return { container, loading, error };
}
