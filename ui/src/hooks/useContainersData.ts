import { useState, useEffect, useCallback, useRef } from 'react';
import { getContainerStatus, getExplorerData, checkContainers } from '../api/client';
import { useEventStream } from './useEventStream';
import { usePeriodicRefresh } from './usePeriodicRefresh';
import type {
  DiscoveryResult,
  ExplorerData,
  UnifiedContainerItem,
  ImageInfo,
  NetworkInfo,
  VolumeInfo,
} from '../types/api';

interface ContainersDataResult {
  containers: UnifiedContainerItem[];
  images: ImageInfo[];
  networks: NetworkInfo[];
  volumes: VolumeInfo[];
  containerStacks: Record<string, UnifiedContainerItem[]>;
  standaloneContainers: UnifiedContainerItem[];
  stats: {
    total: number;
    running: number;
    stopped: number;
    updatesFound: number;
    upToDate: number;
  };
  loading: boolean;
  error: string | null;
  checking: boolean;
  checkProgress: { checked: number; total: number; percent: number; message: string } | null;
  reconnecting: boolean;
  refresh: () => Promise<void>;
  cacheRefresh: () => Promise<void>;
}

function mergeContainerData(
  explorerData: ExplorerData | null,
  statusData: DiscoveryResult | null
): {
  containers: UnifiedContainerItem[];
  containerStacks: Record<string, UnifiedContainerItem[]>;
  standaloneContainers: UnifiedContainerItem[];
} {
  if (!explorerData) {
    return { containers: [], containerStacks: {}, standaloneContainers: [] };
  }

  // Build lookup from status data by container name
  const statusByName = new Map<string, DiscoveryResult['containers'][0]>();
  if (statusData) {
    for (const c of statusData.containers) {
      statusByName.set(c.container_name, c);
    }
  }

  // Merge explorer containers with status data
  const mergeOne = (explorer: ExplorerData['standalone_containers'][0]): UnifiedContainerItem => {
    const status = statusByName.get(explorer.name);
    return {
      // Explorer fields
      id: explorer.id,
      name: explorer.name,
      image: explorer.image,
      state: explorer.state,
      health_status: explorer.health_status,
      stack: explorer.stack,
      created: explorer.created,
      networks: explorer.networks,
      // Status fields (optional)
      container_name: status?.container_name,
      current_tag: status?.current_tag,
      current_version: status?.current_version,
      latest_version: status?.latest_version,
      latest_resolved_version: status?.latest_resolved_version,
      change_type: status?.change_type,
      update_status: status?.status,
      recommended_tag: status?.recommended_tag,
      using_latest_tag: status?.using_latest_tag,
      env_controlled: status?.env_controlled,
      env_var_name: status?.env_var_name,
      compose_image: status?.compose_image,
      pre_update_check_pass: status?.pre_update_check_pass,
      pre_update_check_fail: status?.pre_update_check_fail,
      is_local: status?.is_local,
      error: status?.error,
      labels: status?.labels,
      compose_labels: status?.compose_labels,
      labels_out_of_sync: status?.labels_out_of_sync,
      dependencies: status?.dependencies,
      service: status?.service,
      has_update_data: !!status,
    };
  };

  const containerStacks: Record<string, UnifiedContainerItem[]> = {};
  for (const [stackName, stackContainers] of Object.entries(explorerData.container_stacks || {})) {
    containerStacks[stackName] = stackContainers.map(mergeOne);
  }

  const standaloneContainers = (explorerData.standalone_containers || []).map(mergeOne);

  const containers = [
    ...Object.values(containerStacks).flat(),
    ...standaloneContainers,
  ];

  return { containers, containerStacks, standaloneContainers };
}

export function useContainersData(): ContainersDataResult {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [explorerData, setExplorerData] = useState<ExplorerData | null>(null);
  const [statusData, setStatusData] = useState<DiscoveryResult | null>(null);
  const hasInitialData = useRef(false);

  const { checkProgress, containerUpdated, reconnecting, wasDisconnected, clearWasDisconnected } = useEventStream(true);

  // Fetch both data sources in parallel
  const fetchData = useCallback(async () => {
    try {
      const [explorerRes, statusRes] = await Promise.all([
        getExplorerData(),
        getContainerStatus(),
      ]);

      if (explorerRes.success && explorerRes.data) {
        setExplorerData(explorerRes.data);
      }
      if (statusRes.success && statusRes.data) {
        setStatusData(statusRes.data);
      }

      if (!explorerRes.success && !statusRes.success) {
        if (!hasInitialData.current) {
          setError(explorerRes.error || statusRes.error || 'Failed to fetch data');
        }
      } else {
        setError(null);
        hasInitialData.current = true;
      }
    } catch (err) {
      if (!hasInitialData.current) {
        setError(err instanceof Error ? err.message : 'Unknown error');
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Background refresh — trigger background check, then fetch cached data
  const backgroundRefresh = useCallback(async () => {
    try {
      await fetch('/api/trigger-check', { method: 'POST' });
      await new Promise(resolve => setTimeout(resolve, 500));
      const [explorerRes, statusRes] = await Promise.all([
        getExplorerData(),
        getContainerStatus(),
      ]);
      if (explorerRes.success && explorerRes.data) setExplorerData(explorerRes.data);
      if (statusRes.success && statusRes.data) setStatusData(statusRes.data);
      if (explorerRes.success || statusRes.success) setError(null);
    } catch (err) {
      console.error('Background refresh failed:', err);
    }
  }, []);

  // Cache refresh — clears cache and triggers fresh registry queries
  const cacheRefresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [explorerRes, statusRes] = await Promise.all([
        getExplorerData(),
        checkContainers(),
      ]);
      if (explorerRes.success && explorerRes.data) setExplorerData(explorerRes.data);
      if (statusRes.success && statusRes.data) setStatusData(statusRes.data);
      if (!explorerRes.success && !statusRes.success) {
        setError(explorerRes.error || statusRes.error || 'Failed to refresh');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Periodic background refresh (every 60 seconds)
  usePeriodicRefresh(backgroundRefresh, 60000, true, () => loading);

  // Refresh on visibility/focus changes
  useEffect(() => {
    const mountTimer = setTimeout(backgroundRefresh, 100);
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') backgroundRefresh();
    };
    const handleFocus = () => backgroundRefresh();
    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('focus', handleFocus);
    return () => {
      clearTimeout(mountTimer);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('focus', handleFocus);
    };
  }, [backgroundRefresh]);

  // Auto-refresh on container updated events
  useEffect(() => {
    if (!containerUpdated) return;
    if (containerUpdated.status === 'updated') {
      backgroundRefresh();
    } else {
      fetchData();
    }
  }, [containerUpdated, backgroundRefresh, fetchData]);

  // Auto-refresh on reconnection
  useEffect(() => {
    if (wasDisconnected) {
      backgroundRefresh();
      clearWasDisconnected();
    }
  }, [wasDisconnected, clearWasDisconnected, backgroundRefresh]);

  const merged = mergeContainerData(explorerData, statusData);

  const stats = {
    total: merged.containers.length,
    running: merged.containers.filter(c => c.state === 'running').length,
    stopped: merged.containers.filter(c => c.state !== 'running').length,
    updatesFound: statusData?.updates_found ?? 0,
    upToDate: statusData?.up_to_date ?? 0,
  };

  return {
    containers: merged.containers,
    containerStacks: merged.containerStacks,
    standaloneContainers: merged.standaloneContainers,
    images: explorerData?.images ?? [],
    networks: explorerData?.networks ?? [],
    volumes: explorerData?.volumes ?? [],
    stats,
    loading,
    error,
    checking: statusData?.checking ?? false,
    checkProgress,
    reconnecting,
    refresh: backgroundRefresh,
    cacheRefresh,
  };
}
