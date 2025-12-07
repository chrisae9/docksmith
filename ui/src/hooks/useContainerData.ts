import { useState, useEffect } from 'react';
import { getContainerStatus, getContainerLabels } from '../api/client';
import type { ContainerInfo } from '../types/api';

interface UseContainerDataResult {
  container: ContainerInfo | undefined;
  loading: boolean;
  error: string | null;
}

/**
 * Shared hook for fetching container data with labels.
 * Used by ScriptSelectionPage, TagFilterPage, RestartDependenciesPage.
 */
export function useContainerData(containerName: string | undefined): UseContainerDataResult {
  const [container, setContainer] = useState<ContainerInfo | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!containerName) {
      setLoading(false);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);

        const [statusResponse, labelsResponse] = await Promise.all([
          getContainerStatus(),
          getContainerLabels(containerName),
        ]);

        if (statusResponse.success && statusResponse.data) {
          const foundContainer = statusResponse.data.containers.find(
            (c) => c.container_name === containerName
          );

          if (foundContainer && labelsResponse.success && labelsResponse.data) {
            setContainer({
              ...foundContainer,
              labels: labelsResponse.data.labels || {},
            });
          } else if (foundContainer) {
            setContainer(foundContainer);
          } else {
            setError(`Container "${containerName}" not found`);
          }
        } else {
          setError(statusResponse.error || 'Failed to load container status');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [containerName]);

  return { container, loading, error };
}
