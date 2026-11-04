import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { getContainerStatus, getExplorerData, checkContainers, triggerCheck } from '../api/client';
import { getSnapshot, setSnapshot } from '../api/snapshot';
import { useEventStream } from '../context/EventStreamContext';
import { usePeriodicRefresh } from './usePeriodicRefresh';
import { checkFinished, planRefresh } from './refreshTrigger';
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
  refreshing: boolean;
  lastBackgroundRun?: string;
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
      is_ingress: explorer.is_ingress,
      is_self: explorer.is_self,
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
      using_floating_tag: status?.using_floating_tag,
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
      note: status?.note,
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

export function useContainersData(options: { includeVolumeSizes?: boolean } = {}): ContainersDataResult {
  const { includeVolumeSizes = false } = options;
  const [loading, setLoading] = useState(() => !getSnapshot()?.explorer);
  const [error, setError] = useState<string | null>(null);
  const [explorerData, setExplorerData] = useState<ExplorerData | null>(() => getSnapshot()?.explorer ?? null);
  const [statusData, setStatusData] = useState<DiscoveryResult | null>(() => getSnapshot()?.status ?? null);
  // True while a refresh run started an actual check and is still polling for it.
  // Only the run that received triggered=true sets it; deduped runs never do.
  const [refreshing, setRefreshing] = useState(false);
  const hasInitialData = useRef(!!getSnapshot()?.explorer);
  const mountedRef = useRef(true);
  const inFlightRefresh = useRef<Promise<void> | null>(null);
  const lastRefreshStart = useRef(0);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      inFlightRefresh.current = null;
      lastRefreshStart.current = 0;
    };
  }, []);

  const { checkProgress, containerUpdated, reconnecting, wasDisconnected, clearWasDisconnected } = useEventStream();

  // Fetch both data sources in parallel. They settle independently; the explorer
  // request alone owns the loading gate, and status is optional on top of it.
  const fetchData = useCallback(async () => {
    const explorerPromise = getExplorerData({ includeVolumeSizes })
      .then(res => {
        if (res.success && res.data) {
          setExplorerData(res.data);
          setSnapshot({ explorer: res.data });
          hasInitialData.current = true;
          setError(null);
        } else if (!hasInitialData.current) {
          setError(res.error || 'Failed to fetch container data');
        }
      })
      .catch(err => {
        if (!hasInitialData.current) setError(err instanceof Error ? err.message : 'Unknown error');
      })
      .finally(() => setLoading(false));

    const statusPromise = getContainerStatus()
      .then(res => {
        if (res.success && res.data) {
          setStatusData(res.data);
          setSnapshot({ status: res.data });
        }
      })
      .catch(() => { /* status is optional; explorer already carries the list */ });

    await Promise.allSettled([explorerPromise, statusPromise]);
  }, [includeVolumeSizes]);

  // Background refresh — trigger background check, wait for it to complete, then fetch cached data.
  // Polls /api/status until checking=false and lastBackgroundRun advances past the trigger time,
  // so the UI reflects the post-trigger cache rather than the pre-trigger one.
  const runRefresh = useCallback(async (force: boolean) => {
    try {
      const triggerAt = Date.now();
      const triggerRes = await triggerCheck(force);
      if (!triggerRes.success) {
        console.warn('Background check trigger failed:', triggerRes.error);
      }
      const triggered = triggerRes.success && triggerRes.data?.triggered === true;
      // Surface an in-flight check as soon as this run starts one, so the header can
      // show "Checking…" without waiting for the next /api/status refetch.
      if (triggered && mountedRef.current) setRefreshing(true);

      // A deduplicated trigger that neither started nor joined a check means the
      // cached result is already current; read it immediately instead of polling
      // for a last-run timestamp that cannot advance.
      const plan = planRefresh(triggerRes.success ? triggerRes.data : null);
      // The server stamps last_background_run with second precision on its own
      // clock, so a check that completes inside the trigger's own second never
      // reads as "advanced past triggerAt" and the poll would run to its deadline
      // with the header stuck on "Checking…". A last run that differs from the
      // one the trigger reported is equally proof that our check finished.
      const baselineRun = triggerRes.success ? triggerRes.data?.last_background_run : undefined;
      const pollDeadline = triggerAt + 15000;
      let finalStatus: DiscoveryResult | null = null;
      while (plan.poll && mountedRef.current && Date.now() < pollDeadline) {
        await new Promise(resolve => setTimeout(resolve, 300));
        if (!mountedRef.current) return;
        const statusRes = await getContainerStatus(5000);
        if (!statusRes.success || !statusRes.data) continue;
        const status = statusRes.data;
        const runAdvanced =
          plan.mode === 'triggered' &&
          !status.checking &&
          baselineRun !== undefined &&
          status.last_background_run !== baselineRun;
        if (runAdvanced || checkFinished(plan.mode, triggerAt, status)) {
          finalStatus = status;
          break;
        }
      }
      if (!mountedRef.current) return;

      const explorerRes = await getExplorerData({ includeVolumeSizes });
      let resolvedStatus: DiscoveryResult | null = finalStatus;
      let statusOk = finalStatus !== null;
      if (!resolvedStatus) {
        const statusRes = await getContainerStatus();
        if (statusRes.success && statusRes.data) {
          resolvedStatus = statusRes.data;
          statusOk = true;
        }
      }
      if (!mountedRef.current) return;
      if (explorerRes.success && explorerRes.data) {
        setExplorerData(explorerRes.data);
        setSnapshot({ explorer: explorerRes.data });
      }
      if (resolvedStatus) {
        setStatusData(resolvedStatus);
        setSnapshot({ status: resolvedStatus });
      }
      if (explorerRes.success || statusOk) {
        setError(null);
      } else if (force) {
        // A user-initiated refresh that returned nothing leaves the existing rows
        // on screen; say so instead of letting them look freshly checked.
        setError(explorerRes.error || 'Failed to refresh container data');
      }
    } catch (err) {
      if (!mountedRef.current) return;
      console.error('Background refresh failed:', err);
      if (force) setError(err instanceof Error ? err.message : 'Failed to refresh container data');
    } finally {
      // Covers the poll loop and the post-poll refetch, including every early return.
      // Skipped after unmount, like every other state write in this hook.
      if (mountedRef.current) setRefreshing(false);
    }
  }, [includeVolumeSizes]);

  const startRefresh = useCallback((force: boolean) => {
    if (!mountedRef.current) return Promise.resolve();
    lastRefreshStart.current = Date.now();
    const run = runRefresh(force).finally(() => { inFlightRefresh.current = null; });
    inFlightRefresh.current = run;
    return run;
  }, [runRefresh]);

  // Automatic callers (visibility, focus, periodic) join an in-flight run and are
  // debounced for 1s since the last start. Forced callers (Refresh button, container
  // updated, reconnect) always run: they wait for the in-flight run, then start
  // exactly one trailing run if none was started in the meantime.
  const backgroundRefresh = useCallback((force = false): Promise<void> => {
    const current = inFlightRefresh.current;
    if (current) {
      if (!force) return current;
      return current.then(() => {
        if (inFlightRefresh.current) return inFlightRefresh.current;
        return startRefresh(true);
      });
    }
    if (!force && Date.now() - lastRefreshStart.current < 1000) return Promise.resolve();
    return startRefresh(force);
  }, [startRefresh]);

  const refresh = useCallback(() => backgroundRefresh(true), [backgroundRefresh]);

  // Cache refresh — clears cache and triggers fresh registry queries
  const cacheRefresh = useCallback(async () => {
    if (!hasInitialData.current) setLoading(true);
    setError(null);
    try {
      const [explorerRes, statusRes] = await Promise.all([
        getExplorerData({ includeVolumeSizes }),
        checkContainers(),
      ]);
      if (explorerRes.success && explorerRes.data) {
        setExplorerData(explorerRes.data);
        setSnapshot({ explorer: explorerRes.data });
      }
      if (statusRes.success && statusRes.data) {
        setStatusData(statusRes.data);
        setSnapshot({ status: statusRes.data });
      }
      if (!explorerRes.success && !statusRes.success) {
        setError(explorerRes.error || statusRes.error || 'Failed to refresh');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }, [includeVolumeSizes]);

  // Initial load. Runs once; refetches caused by a change of includeVolumeSizes
  // are handled by the effect below, so guard here to avoid a duplicate fetch.
  const didInitialFetch = useRef(false);
  useEffect(() => {
    if (didInitialFetch.current) return;
    didInitialFetch.current = true;
    fetchData();
  }, [fetchData]);

  // Refetch when the volume-sizes option changes (e.g. switching to the Volumes
  // tab). Skips the initial mount: the initial load above already fetched, and a
  // second request on mount would double the cost of this hook.
  const mountedOnce = useRef(false);
  useEffect(() => {
    if (!mountedOnce.current) {
      mountedOnce.current = true;
      return;
    }
    fetchData();
  }, [includeVolumeSizes, fetchData]);

  // Periodic background refresh, ~1 min. The server's 60 s floor counts from check
  // completion, so a 60 s tick is deduped every other run; 65 s keeps a roughly
  // one-minute cadence while still honouring "at most one check per 60 s".
  usePeriodicRefresh(backgroundRefresh, 65000, true, () => loading);

  // Refresh on visibility/focus changes
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') backgroundRefresh();
    };
    const handleFocus = () => backgroundRefresh();
    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('focus', handleFocus);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('focus', handleFocus);
    };
  }, [backgroundRefresh]);

  // Auto-refresh on container updated events. Guard on event identity: the
  // callbacks below change identity when includeVolumeSizes changes, and
  // re-running this effect for an old event would refetch on every tab switch.
  const handledContainerUpdate = useRef<typeof containerUpdated>(null);
  useEffect(() => {
    if (!containerUpdated || handledContainerUpdate.current === containerUpdated) return;
    handledContainerUpdate.current = containerUpdated;
    if (containerUpdated.status === 'updated') {
      backgroundRefresh(true);
    } else {
      fetchData();
    }
  }, [containerUpdated, backgroundRefresh, fetchData]);

  // Auto-refresh on reconnection
  useEffect(() => {
    if (wasDisconnected) {
      backgroundRefresh(true);
      clearWasDisconnected();
    }
  }, [wasDisconnected, clearWasDisconnected, backgroundRefresh]);

  const merged = useMemo(() => mergeContainerData(explorerData, statusData), [explorerData, statusData]);

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
    refreshing,
    lastBackgroundRun: statusData?.last_background_run,
    checkProgress,
    reconnecting,
    refresh,
    cacheRefresh,
  };
}
