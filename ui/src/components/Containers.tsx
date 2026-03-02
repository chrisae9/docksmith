import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { useFocusTrap } from '../hooks/useFocusTrap';
import { useContainersData } from '../hooks/useContainersData';
import {
  removeImage,
  removeNetwork,
  removeVolume,
  pruneContainers,
  pruneImages,
  pruneNetworks,
  pruneVolumes,
} from '../api/client';
import type {
  UnifiedContainerItem,
  ImageInfo,
  NetworkInfo,
  VolumeInfo,
} from '../types/api';
import { ChangeType } from '../types/api';
import { isUpdatable, isMismatch } from '../utils/status';
import { parseImageRef } from '../utils/registry';
import { useToast } from './Toast';
import {
  SearchBar,
  StackGroup,
  StateIndicator,
  ActionMenuButton,
  ActionMenu,
  ActionMenuItem,
  ActionMenuDivider,
  ConfirmRemove,
} from './shared';
import { ImageItem } from './Explorer/ImageItem';
import { NetworkItem } from './Explorer/NetworkItem';
import { VolumeItem } from './Explorer/VolumeItem';
import {
  ExplorerSettingsMenu,
  DEFAULT_SETTINGS,
  type ExplorerSettings,
} from './Explorer/ExplorerSettings';
import { SkeletonDashboard } from './Skeleton/Skeleton';

// Local storage keys
const STORAGE_KEY_SETTINGS = 'containers_settings';
const STORAGE_KEY_ACTIVE_SUBTAB = 'containers_active_subtab';
const STORAGE_KEY_COLLAPSED = 'containers_collapsed_stacks';
const STORAGE_KEY_DASHBOARD = 'dashboard_settings'; // for migration

type SubTab = 'containers' | 'images' | 'networks' | 'volumes';
type ContainerFilter = 'all' | 'updates';

interface ContainerViewSettings {
  filter: ContainerFilter;
  showIgnored: boolean;
  showLocalImages: boolean;
  showMismatch: boolean;
  showStandalone: boolean;
}

const DEFAULT_CONTAINER_VIEW: ContainerViewSettings = {
  filter: 'updates',
  showIgnored: false,
  showLocalImages: false,
  showMismatch: true,
  showStandalone: false,
};

function loadContainerViewSettings(): ContainerViewSettings {
  const saved = localStorage.getItem('containers_view_settings');
  if (saved) {
    try { return { ...DEFAULT_CONTAINER_VIEW, ...JSON.parse(saved) }; } catch { /* */ }
  }
  // Migrate from old dashboard settings
  const oldDash = localStorage.getItem(STORAGE_KEY_DASHBOARD);
  if (oldDash) {
    try {
      const parsed = JSON.parse(oldDash);
      return {
        filter: parsed.filter || 'updates',
        showIgnored: parsed.showIgnored ?? false,
        showLocalImages: parsed.showLocalImages ?? false,
        showMismatch: parsed.showMismatch ?? true,
        showStandalone: parsed.showStandalone ?? false,
      };
    } catch { /* */ }
  }
  return DEFAULT_CONTAINER_VIEW;
}

export function Containers() {
  const navigate = useNavigate();
  const toast = useToast();

  // Data hook
  const {
    containers,
    containerStacks,
    standaloneContainers,
    images,
    networks,
    volumes,
    loading,
    error,
    checkProgress,
    reconnecting,
    refresh,
    cacheRefresh,
  } = useContainersData();

  // Sub-tab state
  const [activeSubTab, setActiveSubTab] = useState<SubTab>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_ACTIVE_SUBTAB);
    return (saved as SubTab) || 'containers';
  });

  // Container view settings (filter, show ignored, etc.)
  const [viewSettings, setViewSettings] = useState<ContainerViewSettings>(loadContainerViewSettings);

  // Explorer-style settings (groupBy, sortBy, etc.)
  const [explorerSettings, setExplorerSettings] = useState<ExplorerSettings>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_SETTINGS);
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        return {
          containers: { ...DEFAULT_SETTINGS.containers, ...parsed.containers },
          images: { ...DEFAULT_SETTINGS.images, ...parsed.images },
          networks: { ...DEFAULT_SETTINGS.networks, ...parsed.networks },
          volumes: { ...DEFAULT_SETTINGS.volumes, ...parsed.volumes },
        };
      } catch { /* */ }
    }
    return DEFAULT_SETTINGS;
  });

  // UI state
  const [searchQuery, setSearchQuery] = useState('');
  const [collapsedStacks, setCollapsedStacks] = useState<Set<string>>(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY_COLLAPSED);
      return saved ? new Set(JSON.parse(saved)) : new Set();
    } catch {
      return new Set();
    }
  });
  const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
  const [blockedExcluded, setBlockedExcluded] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [activeActionMenu, setActiveActionMenu] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const mainRef = useRef<HTMLElement>(null);

  // Resource action menu state
  const [activeImageMenu, setActiveImageMenu] = useState<string | null>(null);
  const [activeNetworkMenu, setActiveNetworkMenu] = useState<string | null>(null);
  const [activeVolumeMenu, setActiveVolumeMenu] = useState<string | null>(null);
  const [confirmImageRemove, setConfirmImageRemove] = useState<string | null>(null);
  const [confirmNetworkRemove, setConfirmNetworkRemove] = useState<string | null>(null);
  const [confirmVolumeRemove, setConfirmVolumeRemove] = useState<string | null>(null);
  const [imageLoading, setImageLoading] = useState<string | null>(null);
  const [networkLoading, setNetworkLoading] = useState<string | null>(null);
  const [volumeLoading, setVolumeLoading] = useState<string | null>(null);

  // Stack action menu
  const [activeStackMenu, setActiveStackMenu] = useState<string | null>(null);

  // Prune state
  const [pruneLoading, setPruneLoading] = useState(false);
  const [confirmPrune, setConfirmPrune] = useState<SubTab | null>(null);
  const cancelPrune = useCallback(() => setConfirmPrune(null), []);
  const pruneDialogRef = useFocusTrap(!!confirmPrune, cancelPrune);

  // Bulk action state
  const [showBulkActions, setShowBulkActions] = useState(false);

  // Pull-to-refresh
  const [pullDistance, setPullDistance] = useState(0);
  const [isPulling, setIsPulling] = useState(false);
  const pullStartY = useRef<number | null>(null);

  // Save scroll position and navigate to container detail
  const SCROLL_KEY = 'containers_scroll_top';
  const navigateToContainer = useCallback((name: string, state?: Record<string, unknown>) => {
    if (mainRef.current) {
      sessionStorage.setItem(SCROLL_KEY, String(mainRef.current.scrollTop));
    }
    navigate(`/container/${name}`, state ? { state } : undefined);
  }, [navigate]);

  // Restore scroll position after data loads
  useEffect(() => {
    if (!loading) {
      const saved = sessionStorage.getItem(SCROLL_KEY);
      if (saved) {
        sessionStorage.removeItem(SCROLL_KEY);
        requestAnimationFrame(() => {
          if (mainRef.current) {
            mainRef.current.scrollTop = parseInt(saved, 10);
          }
        });
      }
    }
  }, [loading]);

  // Persist settings
  useEffect(() => { localStorage.setItem(STORAGE_KEY_ACTIVE_SUBTAB, activeSubTab); }, [activeSubTab]);
  useEffect(() => { localStorage.setItem(STORAGE_KEY_COLLAPSED, JSON.stringify([...collapsedStacks])); }, [collapsedStacks]);
  useEffect(() => { localStorage.setItem(STORAGE_KEY_SETTINGS, JSON.stringify(explorerSettings)); }, [explorerSettings]);
  useEffect(() => {
    localStorage.setItem('containers_view_settings', JSON.stringify(viewSettings));
    window.dispatchEvent(new Event('viewSettingsChanged'));
  }, [viewSettings]);

  const updateViewSettings = (updates: Partial<ContainerViewSettings>) => {
    setViewSettings(prev => ({ ...prev, ...updates }));
  };

  const closeAllMenus = useCallback(() => {
    setActiveActionMenu(null);
    setConfirmRemove(null);
    setActiveImageMenu(null);
    setActiveNetworkMenu(null);
    setActiveVolumeMenu(null);
    setConfirmImageRemove(null);
    setConfirmNetworkRemove(null);
    setConfirmVolumeRemove(null);
    setActiveStackMenu(null);
  }, []);

  // Document-level menu dismiss: close menus on outside click and prevent click-through.
  // Listeners stay always-attached; a ref tracks menu state so React's batched re-render
  // (which runs between pointerdown and click via microtask) can't remove the click handler.
  const menuDismissFlag = useRef(false);
  const anyMenuOpenRef = useRef(false);
  anyMenuOpenRef.current = !!(activeActionMenu || activeImageMenu || activeNetworkMenu || activeVolumeMenu || activeStackMenu);

  useEffect(() => {
    const onPointerDown = (e: PointerEvent) => {
      if (!anyMenuOpenRef.current) return;
      const target = e.target as Element;
      if (target.closest('.action-menu, .action-menu-btn, .stack-actions')) return;
      menuDismissFlag.current = true;
      closeAllMenus();
    };

    const onClick = (e: MouseEvent) => {
      if (menuDismissFlag.current) {
        menuDismissFlag.current = false;
        e.stopPropagation();
        e.preventDefault();
      }
    };

    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('click', onClick, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('click', onClick, true);
      menuDismissFlag.current = false;
    };
  }, [closeAllMenus]);

  // === Container filtering ===
  const filterContainer = useCallback((c: UnifiedContainerItem) => {
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      if (!c.name.toLowerCase().includes(q) &&
          !c.image.toLowerCase().includes(q) &&
          !(c.stack && c.stack.toLowerCase().includes(q))) {
        return false;
      }
    }
    if (explorerSettings.containers.showRunningOnly && c.state !== 'running') return false;
    // Hide standalone containers (not in a stack) unless toggled on or in "all" view
    if (!c.stack && viewSettings.filter !== 'all' && !viewSettings.showStandalone) return false;
    if (c.has_update_data) {
      if (c.update_status === 'LOCAL_IMAGE' && !viewSettings.showLocalImages) return false;
      if (c.update_status === 'IGNORED' && !viewSettings.showIgnored) return false;
    }
    if (viewSettings.filter === 'updates') {
      if (!c.has_update_data) return false;
      // Build a pseudo-ContainerInfo to use existing status helpers
      if (isUpdatable(c.update_status || '')) return true;
      if (viewSettings.showMismatch && isMismatch(c.update_status || '')) return true;
      return false;
    }
    return true;
  }, [searchQuery, viewSettings, explorerSettings.containers.showRunningOnly]);

  // === Container sorting ===
  const sortContainers = useCallback((items: UnifiedContainerItem[]) => {
    const sorted = [...items];
    const asc = explorerSettings.containers.ascending;
    const { sortBy } = explorerSettings.containers;
    let result: UnifiedContainerItem[];
    switch (sortBy) {
      case 'name':
        result = sorted.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case 'status':
        result = sorted.sort((a, b) => {
          const stateOrder: Record<string, number> = { running: 0, paused: 1, restarting: 2, exited: 3, dead: 4 };
          const diff = (stateOrder[a.state] ?? 5) - (stateOrder[b.state] ?? 5);
          return diff !== 0 ? diff : a.name.localeCompare(b.name);
        });
        break;
      case 'created':
        result = sorted.sort((a, b) => b.created - a.created);
        break;
      case 'image':
        result = sorted.sort((a, b) => a.image.localeCompare(b.image) || a.name.localeCompare(b.name));
        break;
      default:
        result = sorted;
    }
    return asc ? result : result.reverse();
  }, [explorerSettings.containers.sortBy, explorerSettings.containers.ascending]);

  // === Container groups (memoized) ===
  const containerGroups = useMemo(() => {
    const groups: Record<string, UnifiedContainerItem[]> = {};
    const { groupBy } = explorerSettings.containers;

    if (groupBy === 'none') {
      const all = sortContainers(containers.filter(filterContainer));
      if (all.length > 0) groups['All Containers'] = all;
    } else if (groupBy === 'status') {
      for (const c of sortContainers(containers.filter(filterContainer))) {
        const groupName = c.state.charAt(0).toUpperCase() + c.state.slice(1);
        if (!groups[groupName]) groups[groupName] = [];
        groups[groupName].push(c);
      }
    } else if (groupBy === 'image') {
      for (const c of sortContainers(containers.filter(filterContainer))) {
        const imageName = parseImageRef(c.image).repository || c.image;
        if (!groups[imageName]) groups[imageName] = [];
        groups[imageName].push(c);
      }
    } else if (groupBy === 'network') {
      for (const c of sortContainers(containers.filter(filterContainer))) {
        const network = c.networks?.[0] || 'none';
        if (!groups[network]) groups[network] = [];
        groups[network].push(c);
      }
    } else {
      // Stack grouping (default)
      for (const [stackName, stackContainers] of Object.entries(containerStacks)) {
        const filtered = sortContainers(stackContainers.filter(filterContainer));
        if (filtered.length > 0) groups[stackName] = filtered;
      }
      const filteredStandalone = sortContainers(standaloneContainers.filter(filterContainer));
      if (filteredStandalone.length > 0) groups['_standalone'] = filteredStandalone;
    }

    return groups;
  }, [containers, containerStacks, standaloneContainers, filterContainer, sortContainers, explorerSettings.containers.groupBy]);

  // === Counts ===
  const counts = useMemo(() => ({
    containers: containers.length,
    images: images.length,
    networks: networks.length,
    volumes: volumes.length,
  }), [containers.length, images.length, networks.length, volumes.length]);

  // === Selection ===
  const toggleContainer = (name: string) => {
    setSelectedContainers(prev => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
    setBlockedExcluded(false);
  };

  const selectAll = () => {
    const visible = containers.filter(filterContainer);
    const blocked = visible.filter(c => c.update_status === 'UPDATE_AVAILABLE_BLOCKED');
    const nonBlocked = visible.filter(c => c.update_status !== 'UPDATE_AVAILABLE_BLOCKED');
    setSelectedContainers(new Set(nonBlocked.map(c => c.name)));
    setBlockedExcluded(blocked.length > 0);
  };

  const includeBlocked = () => {
    const visible = containers.filter(filterContainer);
    setSelectedContainers(new Set(visible.map(c => c.name)));
    setBlockedExcluded(false);
  };

  const deselectAll = () => {
    setSelectedContainers(new Set());
    setBlockedExcluded(false);
  };

  const toggleStackSelection = (stackContainers: UnifiedContainerItem[]) => {
    const names = stackContainers.map(c => c.name);
    const allSelected = names.every(n => selectedContainers.has(n));
    setSelectedContainers(prev => {
      const next = new Set(prev);
      names.forEach(n => allSelected ? next.delete(n) : next.add(n));
      return next;
    });
  };

  // === Stack toggling ===
  const toggleStack = (name: string) => {
    setCollapsedStacks(prev => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  };

  const toggleAllStacks = () => {
    if (collapsedStacks.size > 0) {
      setCollapsedStacks(new Set());
    } else {
      setCollapsedStacks(new Set(Object.keys(containerGroups)));
    }
  };

  // === Pull-to-refresh ===
  const handleTouchStart = (e: React.TouchEvent) => {
    const scrollTop = mainRef.current?.scrollTop ?? 0;
    if (scrollTop === 0 && !loading) {
      pullStartY.current = e.touches[0].clientY;
      setIsPulling(true);
    }
  };
  const handleTouchMove = (e: React.TouchEvent) => {
    if (!isPulling || pullStartY.current === null) return;
    const distance = e.touches[0].clientY - pullStartY.current;
    if (distance > 0) setPullDistance(Math.min(distance, 150));
  };
  const handleTouchEnd = () => {
    if (pullDistance >= 120) cacheRefresh();
    else if (pullDistance >= 70) refresh();
    setIsPulling(false);
    setPullDistance(0);
    pullStartY.current = null;
  };

  // === Container actions ===
  const handleContainerAction = (name: string, action: 'start' | 'stop' | 'restart' | 'remove', force?: boolean) => {
    setActiveActionMenu(null);
    setConfirmRemove(null);
    navigate('/operation', { state: { [action]: { containerName: name, force } } });
  };

  const handleStackAction = (stackName: string, action: 'restart' | 'stop') => {
    const stackContainers = containerGroups[stackName];
    if (!stackContainers?.length) return;
    setActiveStackMenu(null);
    const key = action === 'restart' ? 'stackRestart' : 'stackStop';
    navigate('/operation', {
      state: { [key]: { stackName, containers: stackContainers.map(c => c.name) } }
    });
  };

  // === Bulk actions ===
  const handleBulkUpdate = () => {
    if (selectedContainers.size === 0) return;
    const allSelected = containers.filter(c => selectedContainers.has(c.name));
    const selected = allSelected.filter(c => c.has_update_data);
    const skippedNoData = allSelected.length - selected.length;

    const mismatchContainers: string[] = [];
    const containersToUpdate: Array<{ name: string; target_version: string; stack: string; force?: boolean; change_type: number; old_resolved_version: string; new_resolved_version: string }> = [];

    for (const c of selected) {
      if (isMismatch(c.update_status || '')) {
        mismatchContainers.push(c.name);
      } else if (isUpdatable(c.update_status || '')) {
        containersToUpdate.push({
          name: c.name,
          target_version: c.recommended_tag || c.latest_version || '',
          stack: c.stack || '',
          force: c.update_status === 'UPDATE_AVAILABLE_BLOCKED' || undefined,
          change_type: c.change_type || 0,
          old_resolved_version: c.current_version || '',
          new_resolved_version: c.latest_resolved_version || c.latest_version || '',
        });
      }
    }

    const skippedUpToDate = selected.length - containersToUpdate.length - mismatchContainers.length;
    const totalSkipped = skippedNoData + skippedUpToDate;

    if (containersToUpdate.length === 0 && mismatchContainers.length === 0) {
      setSelectedContainers(new Set());
      if (skippedNoData > 0) {
        toast.info(`${skippedNoData} container${skippedNoData > 1 ? 's' : ''} not yet scanned — run a check first`);
      } else {
        toast.info('All selected containers are already up to date');
      }
      return;
    }

    // Notify about skipped containers before navigating
    if (totalSkipped > 0) {
      const parts: string[] = [];
      if (skippedNoData > 0) parts.push(`${skippedNoData} not scanned`);
      if (skippedUpToDate > 0) parts.push(`${skippedUpToDate} up to date`);
      toast.info(`Skipped ${totalSkipped}: ${parts.join(', ')}`);
    }

    setSelectedContainers(new Set());

    if (mismatchContainers.length > 0 && containersToUpdate.length > 0) {
      navigate('/operation', { state: { mixed: { updates: containersToUpdate, mismatches: mismatchContainers } } });
    } else if (mismatchContainers.length > 0) {
      if (mismatchContainers.length === 1) {
        navigate('/operation', { state: { fixMismatch: { containerName: mismatchContainers[0] } } });
      } else {
        navigate('/operation', { state: { batchFixMismatch: { containerNames: mismatchContainers } } });
      }
    } else if (containersToUpdate.length > 0) {
      navigate('/operation', { state: { update: { containers: containersToUpdate } } });
    }
  };

  const handleBulkAction = (action: 'start' | 'stop' | 'restart' | 'remove') => {
    const names = Array.from(selectedContainers);
    setSelectedContainers(new Set());
    setShowBulkActions(false);

    // Navigate through the operation page
    if (names.length === 1) {
      navigate('/operation', { state: { [action]: { containerName: names[0] } } });
    } else {
      // For multi-container ops, use batch endpoints
      const batchKey = `batch${action.charAt(0).toUpperCase()}${action.slice(1)}`;
      navigate('/operation', { state: { [batchKey]: { containers: names } } });
    }
  };

  const handleBulkLabel = (labelOp: { ignore?: boolean; allow_latest?: boolean; version_pin_major?: boolean; version_pin_minor?: boolean; version_pin_patch?: boolean; tag_regex?: string; script?: string }) => {
    const names = Array.from(selectedContainers);
    setSelectedContainers(new Set());
    setShowBulkActions(false);
    navigate('/operation', { state: { batchLabel: { containers: names, labelOp } } });
  };

  // === Resource actions (images/networks/volumes) ===
  const handleImageRemove = async (imageId: string, force?: boolean) => {
    setImageLoading(imageId); setActiveImageMenu(null); setConfirmImageRemove(null);
    try {
      const r = await removeImage(imageId, { force });
      if (r.success) { toast.success('Image removed'); refresh(); } else toast.error(r.error || 'Failed');
    } catch (err) { toast.error(err instanceof Error ? err.message : 'Failed'); }
    finally { setImageLoading(null); }
  };
  const handleNetworkRemove = async (networkId: string) => {
    setNetworkLoading(networkId); setActiveNetworkMenu(null); setConfirmNetworkRemove(null);
    try {
      const r = await removeNetwork(networkId);
      if (r.success) { toast.success('Network removed'); refresh(); } else toast.error(r.error || 'Failed');
    } catch (err) { toast.error(err instanceof Error ? err.message : 'Failed'); }
    finally { setNetworkLoading(null); }
  };
  const handleVolumeRemove = async (volumeName: string, force?: boolean) => {
    setVolumeLoading(volumeName); setActiveVolumeMenu(null); setConfirmVolumeRemove(null);
    try {
      const r = await removeVolume(volumeName, { force });
      if (r.success) { toast.success('Volume removed'); refresh(); } else toast.error(r.error || 'Failed');
    } catch (err) { toast.error(err instanceof Error ? err.message : 'Failed'); }
    finally { setVolumeLoading(null); }
  };

  // === Prune ===
  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  const handlePrune = async (tab: SubTab) => {
    setPruneLoading(true); setConfirmPrune(null);
    try {
      let result;
      switch (tab) {
        case 'containers': result = await pruneContainers(); break;
        case 'images': result = await pruneImages(); break;
        case 'networks': result = await pruneNetworks(); break;
        case 'volumes': result = await pruneVolumes(); break;
      }
      if (result.success && result.data) {
        const count = result.data.items_deleted?.length || 0;
        const space = result.data.space_reclaimed;
        let message = result.data.message || `Pruned ${count} ${tab}`;
        if (space && space > 0) message += ` (${formatBytes(space)} reclaimed)`;
        toast.success(message);
        refresh();
      } else toast.error(result.error || `Failed to prune ${tab}`);
    } catch (err) { toast.error(err instanceof Error ? err.message : `Failed to prune ${tab}`); }
    finally { setPruneLoading(false); }
  };

  // === Image/Network/Volume groups (from Explorer) ===
  const getRepository = (image: ImageInfo): string => {
    const tag = image.tags?.[0];
    if (!tag || tag === '<none>') return '<none>';
    return parseImageRef(tag).repository || '<none>';
  };

  const sortImages = useCallback((imgs: ImageInfo[]) => {
    const sorted = [...imgs];
    const asc = explorerSettings.images.ascending;
    let result: ImageInfo[];
    switch (explorerSettings.images.sortBy) {
      case 'tags': result = sorted.sort((a, b) => (a.tags?.[0] || '<none>').localeCompare(b.tags?.[0] || '<none>')); break;
      case 'size': result = sorted.sort((a, b) => b.size - a.size); break;
      case 'created': result = sorted.sort((a, b) => b.created - a.created); break;
      case 'id': result = sorted.sort((a, b) => a.id.localeCompare(b.id)); break;
      default: result = sorted;
    }
    return asc ? result : result.reverse();
  }, [explorerSettings.images.sortBy, explorerSettings.images.ascending]);

  const sortNetworks = useCallback((nets: NetworkInfo[]) => {
    const sorted = [...nets];
    const asc = explorerSettings.networks.ascending;
    let result: NetworkInfo[];
    switch (explorerSettings.networks.sortBy) {
      case 'name': result = sorted.sort((a, b) => a.name.localeCompare(b.name)); break;
      case 'driver': result = sorted.sort((a, b) => a.driver.localeCompare(b.driver) || a.name.localeCompare(b.name)); break;
      case 'containers': result = sorted.sort((a, b) => (b.containers?.length || 0) - (a.containers?.length || 0)); break;
      case 'created': result = sorted.sort((a, b) => b.created - a.created); break;
      default: result = sorted;
    }
    return asc ? result : result.reverse();
  }, [explorerSettings.networks.sortBy, explorerSettings.networks.ascending]);

  const sortVolumes = useCallback((vols: VolumeInfo[]) => {
    const sorted = [...vols];
    const asc = explorerSettings.volumes.ascending;
    let result: VolumeInfo[];
    switch (explorerSettings.volumes.sortBy) {
      case 'name': result = sorted.sort((a, b) => a.name.localeCompare(b.name)); break;
      case 'size': result = sorted.sort((a, b) => (b.size || 0) - (a.size || 0)); break;
      case 'created': result = sorted.sort((a, b) => b.created - a.created); break;
      case 'driver': result = sorted.sort((a, b) => a.driver.localeCompare(b.driver) || a.name.localeCompare(b.name)); break;
      default: result = sorted;
    }
    return asc ? result : result.reverse();
  }, [explorerSettings.volumes.sortBy, explorerSettings.volumes.ascending]);

  const filterItems = <T extends { name?: string; tags?: string[] }>(items: T[], query: string, getText: (item: T) => string): T[] => {
    if (!query) return items;
    const q = query.toLowerCase();
    return items.filter(item => getText(item).toLowerCase().includes(q));
  };

  const imageGroups = useMemo(() => {
    let imgs = filterItems(images, searchQuery, i => (i.tags || []).join(' '));
    if (explorerSettings.images.showDanglingOnly) imgs = imgs.filter(i => i.dangling);
    const sorted = sortImages(imgs);
    if (explorerSettings.images.groupBy === 'none') return { 'All Images': sorted };
    const groups: Record<string, ImageInfo[]> = {};
    for (const img of sorted) {
      const group = img.dangling ? 'Dangling' : getRepository(img);
      if (!groups[group]) groups[group] = [];
      groups[group].push(img);
    }
    return groups;
  }, [images, searchQuery, explorerSettings.images, sortImages]);

  const networkGroups = useMemo(() => {
    let nets = filterItems(networks, searchQuery, n => `${n.name} ${n.driver}`);
    if (explorerSettings.networks.hideBuiltIn) nets = nets.filter(n => !n.is_default);
    const sorted = sortNetworks(nets);
    if (explorerSettings.networks.groupBy === 'none') return { 'All Networks': sorted };
    const groups: Record<string, NetworkInfo[]> = {};
    for (const net of sorted) {
      const driver = net.driver || 'unknown';
      if (!groups[driver]) groups[driver] = [];
      groups[driver].push(net);
    }
    return groups;
  }, [networks, searchQuery, explorerSettings.networks, sortNetworks]);

  const volumeGroups = useMemo(() => {
    let vols = filterItems(volumes, searchQuery, v => `${v.name} ${v.driver} ${(v.containers || []).join(' ')}`);
    if (explorerSettings.volumes.showUnusedOnly) vols = vols.filter(v => !v.containers || v.containers.length === 0);
    const sorted = sortVolumes(vols);
    if (explorerSettings.volumes.groupBy === 'none') return { 'All Volumes': sorted };
    const groups: Record<string, VolumeInfo[]> = {};
    for (const vol of sorted) {
      const isAnon = /^[0-9a-f]{64}$/.test(vol.name);
      const group = isAnon ? 'Anonymous' : 'Named';
      if (!groups[group]) groups[group] = [];
      groups[group].push(vol);
    }
    return groups;
  }, [volumes, searchQuery, explorerSettings.volumes, sortVolumes]);

  // === Version display (from Dashboard's ContainerRow) ===
  const getVersion = (c: UnifiedContainerItem): string => {
    if (!c.has_update_data) return c.image;

    if (c.update_status === 'UP_TO_DATE_PINNABLE' && c.recommended_tag) {
      const currentTag = c.using_latest_tag ? 'latest' : (c.current_tag || 'untagged');
      return `${currentTag} \u2192 ${c.recommended_tag}`;
    }

    if (isUpdatable(c.update_status || '') && c.latest_version) {
      const currentTag = c.current_tag || '';
      const currentResolved = c.current_version || '';
      let currentDisplay: string;
      if (currentTag && currentResolved && currentTag !== currentResolved && !currentTag.includes(currentResolved)) {
        currentDisplay = `${currentTag} (${currentResolved})`;
      } else {
        currentDisplay = currentTag || currentResolved || 'current';
      }
      const latestTag = c.latest_version;
      const latestResolved = c.latest_resolved_version || '';
      let latestDisplay: string;
      if (latestTag && latestResolved && latestTag !== latestResolved && !latestTag.includes(latestResolved)) {
        latestDisplay = `${latestTag} (${latestResolved})`;
      } else {
        latestDisplay = latestTag;
      }
      return `${currentDisplay} \u2192 ${latestDisplay}`;
    }

    if (c.update_status === 'LOCAL_IMAGE') return 'Local image';
    if (c.update_status === 'COMPOSE_MISMATCH') {
      const runningTag = parseImageRef(c.image).tag || c.current_tag || 'unknown';
      const composeTag = c.compose_image ? (parseImageRef(c.compose_image).tag || c.compose_image) : 'unknown';
      return `${runningTag} \u2192 ${composeTag}`;
    }
    if (c.update_status === 'IGNORED') return 'Ignored';
    return c.current_version || c.current_tag || c.image;
  };

  // === Status badge (from Dashboard's ContainerRow) ===
  const getStatusBadge = (c: UnifiedContainerItem) => {
    if (!c.has_update_data) {
      if (c.state !== 'running') return <span className="status-badge stopped">{c.state.toUpperCase()}</span>;
      return null;
    }
    switch (c.update_status) {
      case 'UPDATE_AVAILABLE':
        if (c.change_type === ChangeType.MajorChange) return <span className="status-badge major" title="Major update">MAJOR</span>;
        if (c.change_type === ChangeType.MinorChange) return <span className="status-badge minor" title="Minor update">MINOR</span>;
        if (c.change_type === ChangeType.PatchChange) return <span className="status-badge patch" title="Patch update">PATCH</span>;
        return <span className="status-badge rebuild" title="Update available">REBUILD</span>;
      case 'UPDATE_AVAILABLE_BLOCKED': return <span className="status-badge blocked" title="Update blocked">BLOCKED</span>;
      case 'UP_TO_DATE':
        if (c.state !== 'running') return <span className="status-badge stopped">{c.state.toUpperCase()}</span>;
        return <span className="status-badge current" title="Up to date">CURRENT</span>;
      case 'UP_TO_DATE_PINNABLE':
        return <span className="status-badge pin" title="No version tag specified">PIN</span>;
      case 'LOCAL_IMAGE': return <span className="status-badge local" title="Local image">LOCAL</span>;
      case 'COMPOSE_MISMATCH': return <span className="status-badge mismatch" title="Running image differs from compose">MISMATCH</span>;
      case 'IGNORED': return <span className="status-badge ignored" title="Ignored">IGNORED</span>;
      default:
        if (c.state !== 'running') return <span className="status-badge stopped">{c.state.toUpperCase()}</span>;
        return null;
    }
  };

  // === Get group icon ===
  const getGroupIcon = (groupName: string): string => {
    if (explorerSettings.containers.groupBy === 'status') {
      const icons: Record<string, string> = { Running: 'fa-circle-play', Paused: 'fa-circle-pause', Restarting: 'fa-rotate', Exited: 'fa-circle-stop', Dead: 'fa-skull', Created: 'fa-circle-plus' };
      return icons[groupName] || 'fa-box';
    }
    if (explorerSettings.containers.groupBy === 'none') return 'fa-boxes-stacked';
    return groupName === '_standalone' ? 'fa-box' : 'fa-layer-group';
  };

  // === Render unified container row ===
  const renderContainerRow = (c: UnifiedContainerItem) => {
    const isActive = activeActionMenu === c.name;
    const isRunning = c.state === 'running';
    const hasUpdate = c.has_update_data && isUpdatable(c.update_status || '');
    const hasMismatch = c.has_update_data && isMismatch(c.update_status || '');

    // Label indicators
    const hasTagRegex = !!c.labels?.['docksmith.tag-regex'];
    const hasPreUpdateScript = !!c.labels?.['docksmith.pre-update-check'];
    const allowsLatest = c.labels?.['docksmith.allow-latest'] === 'true';
    const versionPinMajor = c.labels?.['docksmith.version-pin-major'] === 'true';
    const versionPinMinor = c.labels?.['docksmith.version-pin-minor'] === 'true';
    const versionPinPatch = c.labels?.['docksmith.version-pin-patch'] === 'true';
    const versionPin = versionPinMajor ? 'major' : versionPinMinor ? 'minor' : versionPinPatch ? 'patch' : null;

    return (
      <li
        key={c.name}
        className={`unified-row ${selectedContainers.has(c.name) ? 'selected' : ''} ${hasUpdate ? 'has-update' : ''} ${hasMismatch ? 'has-mismatch' : ''}`}
      >
        <label className="checkbox-zone" onClick={(e) => { e.preventDefault(); toggleContainer(c.name); }}>
          <input
            type="checkbox"
            className="row-checkbox"
            checked={selectedContainers.has(c.name)}
            readOnly
            aria-label={`Select ${c.name}`}
          />
          <StateIndicator state={c.state} healthStatus={c.health_status} isLoading={false} />
        </label>
        <div
          className="row-link"
          role="button"
          tabIndex={0}
          onClick={() => navigateToContainer(c.name)}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); navigateToContainer(c.name); } }}
        >
          <div className="container-info">
            <span className="name">{c.name}</span>
            <span className="version">{getVersion(c)}</span>
          </div>
          {c.pre_update_check_pass && <span className="check" title="Pre-update check passed"><i className="fa-solid fa-check"></i></span>}
          {c.pre_update_check_fail && <span className="warn" title={c.pre_update_check_fail}><i className="fa-solid fa-triangle-exclamation"></i></span>}
          {c.health_status === 'unhealthy' && <span className="warn" title="Container is unhealthy"><i className="fa-solid fa-heart-crack"></i></span>}
          {c.env_controlled && <span className="label-icon env" title={`Image from .env: $${c.env_var_name || 'ENV'}`}><i className="fa-solid fa-file-code"></i></span>}
          {versionPin && <span className={`label-icon pin-${versionPin}`} title={`Version pinned to ${versionPin}`}><i className="fa-solid fa-thumbtack"></i></span>}
          {hasTagRegex && <span className="label-icon regex" title="Tag regex filter"><i className="fa-solid fa-filter"></i></span>}
          {hasPreUpdateScript && !c.pre_update_check_pass && !c.pre_update_check_fail && <span className="label-icon script" title="Pre-update script"><i className="fa-solid fa-terminal"></i></span>}
          {allowsLatest && <span className="label-icon latest" title="Allows :latest"><i className="fa-solid fa-tag"></i></span>}
          {c.note && <span className="label-icon ghost" title={c.note}><i className="fa-solid fa-ghost"></i></span>}
          {getStatusBadge(c)}
        </div>
        <div className="item-actions">
          <ActionMenuButton isActive={isActive} isLoading={false} onClick={() => { setActiveActionMenu(isActive ? null : c.name); setConfirmRemove(null); }} />
          <ActionMenu isActive={isActive}>
            {isRunning ? (
              <>
                <ActionMenuItem icon="fa-stop" label="Stop" onClick={() => handleContainerAction(c.name, 'stop')} />
                <ActionMenuItem icon="fa-rotate" label="Restart" onClick={() => handleContainerAction(c.name, 'restart')} />
              </>
            ) : (
              <ActionMenuItem icon="fa-play" label="Start" onClick={() => handleContainerAction(c.name, 'start')} />
            )}
            {hasUpdate && <ActionMenuItem icon="fa-arrow-up" label={c.update_status === 'UPDATE_AVAILABLE_BLOCKED' ? 'Force Update' : 'Update'} onClick={() => {
              setActiveActionMenu(null);
              navigate('/operation', { state: { update: { containers: [{ name: c.name, target_version: c.recommended_tag || c.latest_version || '', stack: c.stack || '', force: c.update_status === 'UPDATE_AVAILABLE_BLOCKED', change_type: c.change_type || 0, old_resolved_version: c.current_version || '', new_resolved_version: c.latest_resolved_version || c.latest_version || '' }] } } });
            }} />}
            {hasMismatch && <ActionMenuItem icon="fa-arrows-rotate" label="Fix Mismatch" onClick={() => {
              setActiveActionMenu(null);
              navigate('/operation', { state: { fixMismatch: { containerName: c.name } } });
            }} />}
            <ActionMenuDivider />
            <ActionMenuItem icon="fa-sliders" label="Configure..." onClick={() => { setActiveActionMenu(null); navigateToContainer(c.name, { tab: 'config' }); }} />
            <ActionMenuItem icon="fa-file-lines" label="Logs" onClick={() => { setActiveActionMenu(null); navigateToContainer(c.name, { tab: 'logs' }); }} />
            <ActionMenuItem icon="fa-magnifying-glass" label="Inspect" onClick={() => { setActiveActionMenu(null); navigateToContainer(c.name, { tab: 'inspect' }); }} />
            <ActionMenuDivider />
            {confirmRemove === c.name ? (
              <ConfirmRemove onConfirm={() => handleContainerAction(c.name, 'remove', true)} onCancel={() => { setActiveActionMenu(null); setConfirmRemove(null); }} />
            ) : (
              <ActionMenuItem icon="fa-trash" label="Remove" onClick={() => setConfirmRemove(c.name)} danger />
            )}
          </ActionMenu>
        </div>
      </li>
    );
  };

  // === Render containers tab ===
  const renderContainersTab = () => {
    const groupEntries = Object.entries(containerGroups);
    const sortedGroups = explorerSettings.containers.groupBy === 'status'
      ? groupEntries.sort(([a], [b]) => {
          const order = ['Running', 'Paused', 'Restarting', 'Created', 'Exited', 'Dead'];
          return order.indexOf(a) - order.indexOf(b);
        })
      : explorerSettings.containers.groupBy === 'stack'
        ? groupEntries.sort(([a], [b]) => {
            if (a === '_standalone') return 1;
            if (b === '_standalone') return -1;
            return a.localeCompare(b);
          })
        : groupEntries;

    if (groupEntries.length === 0) {
      if (searchQuery) {
        return <div className="empty-state"><i className="fa-solid fa-magnifying-glass"></i><h2>No search results</h2><p>No containers match "{searchQuery}"</p></div>;
      }
      if (viewSettings.filter === 'updates') {
        return <div className="empty-state"><i className="fa-solid fa-circle-check"></i><h2>All containers are up to date</h2><p>There are no updates available</p></div>;
      }
      return <div className="empty-state"><i className="fa-solid fa-box-open"></i><h2>No containers found</h2><p>No containers match the current filter</p></div>;
    }

    const hasActiveContainers = (items: UnifiedContainerItem[]) => items.some(c => c.state === 'running' || c.state === 'restarting');

    return (
      <div className="explorer-list-container">
        {sortedGroups.map(([groupName, items]) => (
          <StackGroup
            key={groupName}
            name={groupName === '_standalone' ? 'Standalone' : groupName}
            isCollapsed={collapsedStacks.has(groupName)}
            onToggle={() => toggleStack(groupName)}
            count={items.length}
            icon={getGroupIcon(groupName)}
            isStandalone={groupName === '_standalone' || explorerSettings.containers.groupBy === 'none'}
            listClassName="explorer-list"
            id={`stack-${groupName}`}
            actions={
              <>
                {explorerSettings.containers.groupBy === 'stack' && groupName !== '_standalone' && containerStacks[groupName] && items.length < containerStacks[groupName].length && (
                  <button
                    className="show-full-stack-btn"
                    onClick={(e) => {
                      e.stopPropagation();
                      const stackId = `stack-${groupName}`;
                      if (searchQuery) setSearchQuery('');
                      const updates: Partial<ContainerViewSettings> = {};
                      if (viewSettings.filter === 'updates') updates.filter = 'all';
                      if (!viewSettings.showIgnored && containerStacks[groupName].some(c => c.update_status === 'IGNORED')) updates.showIgnored = true;
                      if (!viewSettings.showLocalImages && containerStacks[groupName].some(c => c.update_status === 'LOCAL_IMAGE')) updates.showLocalImages = true;
                      if (Object.keys(updates).length > 0) updateViewSettings(updates);
                      if (explorerSettings.containers.showRunningOnly && containerStacks[groupName].some(c => c.state !== 'running')) {
                        setExplorerSettings(s => ({ ...s, containers: { ...s.containers, showRunningOnly: false } }));
                      }
                      setCollapsedStacks(prev => {
                        const next = new Set(prev);
                        next.delete(groupName);
                        return next;
                      });
                      requestAnimationFrame(() => {
                        const el = document.getElementById(stackId);
                        if (el) {
                          el.scrollIntoView({ behavior: 'smooth', block: 'start' });
                          el.classList.add('stack-highlight');
                          setTimeout(() => el.classList.remove('stack-highlight'), 1500);
                        }
                      });
                    }}
                    title={`Show all ${containerStacks[groupName].length} containers in ${groupName}`}
                  >
                    Show all {containerStacks[groupName].length}
                  </button>
                )}
                {items.length > 0 && (
                  <button
                    className="stack-select-btn"
                    onClick={(e) => { e.stopPropagation(); toggleStackSelection(items); }}
                    title={items.every(c => selectedContainers.has(c.name)) ? 'Deselect all' : 'Select all'}
                  >
                    <i className={`fa-solid ${items.every(c => selectedContainers.has(c.name)) ? 'fa-square-minus' : 'fa-square-check'}`}></i>
                  </button>
                )}
                {explorerSettings.containers.groupBy === 'stack' && groupName !== '_standalone' && (() => {
                  const isMenuActive = activeStackMenu === groupName;
                  return (
                    <div className="stack-actions" onClick={(e) => e.stopPropagation()}>
                      <ActionMenuButton isActive={isMenuActive} isLoading={false} onClick={(e) => { e.stopPropagation(); setActiveStackMenu(isMenuActive ? null : groupName); }} />
                      <ActionMenu isActive={isMenuActive}>
                        {hasActiveContainers(items) && (
                          <ActionMenuItem icon="fa-rotate" label="Restart Stack" onClick={() => handleStackAction(groupName, 'restart')} />
                        )}
                        {!hasActiveContainers(items) && (
                          <ActionMenuItem icon="fa-rotate" label="Start Stack" onClick={() => handleStackAction(groupName, 'restart')} title="Start all containers" />
                        )}
                        <ActionMenuItem icon="fa-stop" label="Stop Stack" onClick={() => handleStackAction(groupName, 'stop')} />
                      </ActionMenu>
                    </div>
                  );
                })()}
              </>
            }
          >
            {items.map(renderContainerRow)}
          </StackGroup>
        ))}
      </div>
    );
  };

  // === Render resource tabs (reuse from Explorer) ===
  const renderImagesTab = () => {
    const groupEntries = Object.entries(imageGroups).sort(([a], [b]) => {
      if (a === 'Dangling') return 1; if (b === 'Dangling') return -1; return a.localeCompare(b);
    });
    const showGroups = explorerSettings.images.groupBy !== 'none' && groupEntries.length > 0;
    const flatImages = Object.values(imageGroups).flat();

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {flatImages.map(img => (
            <ImageItem key={img.id} image={img} isActive={activeImageMenu === img.id} isLoading={imageLoading === img.id} confirmRemove={confirmImageRemove === img.id}
              onMenuToggle={() => { setActiveImageMenu(activeImageMenu === img.id ? null : img.id); setConfirmImageRemove(null); }}
              onRemove={(f?: boolean) => handleImageRemove(img.id, f)} onConfirmRemove={() => setConfirmImageRemove(img.id)} />
          ))}
          {flatImages.length === 0 && <li className="explorer-empty">No images found</li>}
        </ul>
      );
    }
    return (
      <div className="explorer-list-container">
        {groupEntries.map(([name, imgs]) => (
          <StackGroup key={name} name={name} isCollapsed={collapsedStacks.has(`img_${name}`)} onToggle={() => toggleStack(`img_${name}`)} count={imgs.length}
            icon={name === 'Dangling' ? 'fa-ghost' : 'fa-cube'} isStandalone={false} listClassName="explorer-list">
            {imgs.map(img => (
              <ImageItem key={img.id} image={img} isActive={activeImageMenu === img.id} isLoading={imageLoading === img.id} confirmRemove={confirmImageRemove === img.id}
                onMenuToggle={() => { setActiveImageMenu(activeImageMenu === img.id ? null : img.id); setConfirmImageRemove(null); }}
                onRemove={(f?: boolean) => handleImageRemove(img.id, f)} onConfirmRemove={() => setConfirmImageRemove(img.id)} />
            ))}
          </StackGroup>
        ))}
      </div>
    );
  };

  const renderNetworksTab = () => {
    const groupEntries = Object.entries(networkGroups).sort(([a], [b]) => a.localeCompare(b));
    const showGroups = explorerSettings.networks.groupBy !== 'none' && groupEntries.length > 0;
    const flatNets = Object.values(networkGroups).flat();

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {flatNets.map(n => (
            <NetworkItem key={n.id} network={n} isActive={activeNetworkMenu === n.id} isLoading={networkLoading === n.id} confirmRemove={confirmNetworkRemove === n.id}
              onMenuToggle={() => { setActiveNetworkMenu(activeNetworkMenu === n.id ? null : n.id); setConfirmNetworkRemove(null); }}
              onRemove={() => handleNetworkRemove(n.id)} onConfirmRemove={() => setConfirmNetworkRemove(n.id)} />
          ))}
          {flatNets.length === 0 && <li className="explorer-empty">No networks found</li>}
        </ul>
      );
    }
    return (
      <div className="explorer-list-container">
        {groupEntries.map(([name, nets]) => (
          <StackGroup key={name} name={name} isCollapsed={collapsedStacks.has(`net_${name}`)} onToggle={() => toggleStack(`net_${name}`)} count={nets.length}
            icon="fa-network-wired" isStandalone={false} listClassName="explorer-list">
            {nets.map(n => (
              <NetworkItem key={n.id} network={n} isActive={activeNetworkMenu === n.id} isLoading={networkLoading === n.id} confirmRemove={confirmNetworkRemove === n.id}
                onMenuToggle={() => { setActiveNetworkMenu(activeNetworkMenu === n.id ? null : n.id); setConfirmNetworkRemove(null); }}
                onRemove={() => handleNetworkRemove(n.id)} onConfirmRemove={() => setConfirmNetworkRemove(n.id)} />
            ))}
          </StackGroup>
        ))}
      </div>
    );
  };

  const renderVolumesTab = () => {
    const groupEntries = Object.entries(volumeGroups).sort(([a], [b]) => { if (a === 'Named') return -1; if (b === 'Named') return 1; return a.localeCompare(b); });
    const showGroups = explorerSettings.volumes.groupBy !== 'none' && groupEntries.length > 0;
    const flatVols = Object.values(volumeGroups).flat();

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {flatVols.map(v => (
            <VolumeItem key={v.name} volume={v} isActive={activeVolumeMenu === v.name} isLoading={volumeLoading === v.name} confirmRemove={confirmVolumeRemove === v.name}
              onMenuToggle={() => { setActiveVolumeMenu(activeVolumeMenu === v.name ? null : v.name); setConfirmVolumeRemove(null); }}
              onRemove={(f?: boolean) => handleVolumeRemove(v.name, f)} onConfirmRemove={() => setConfirmVolumeRemove(v.name)} />
          ))}
          {flatVols.length === 0 && <li className="explorer-empty">No volumes found</li>}
        </ul>
      );
    }
    return (
      <div className="explorer-list-container">
        {groupEntries.map(([name, vols]) => (
          <StackGroup key={name} name={name} isCollapsed={collapsedStacks.has(`vol_${name}`)} onToggle={() => toggleStack(`vol_${name}`)} count={vols.length}
            icon={name === 'Anonymous' ? 'fa-fingerprint' : 'fa-hard-drive'} isStandalone={false} listClassName="explorer-list">
            {vols.map(v => (
              <VolumeItem key={v.name} volume={v} isActive={activeVolumeMenu === v.name} isLoading={volumeLoading === v.name} confirmRemove={confirmVolumeRemove === v.name}
                onMenuToggle={() => { setActiveVolumeMenu(activeVolumeMenu === v.name ? null : v.name); setConfirmVolumeRemove(null); }}
                onRemove={(f?: boolean) => handleVolumeRemove(v.name, f)} onConfirmRemove={() => setConfirmVolumeRemove(v.name)} />
            ))}
          </StackGroup>
        ))}
      </div>
    );
  };

  // === Sub-tab definitions ===
  const subTabs: { id: SubTab; label: string; icon: string; count: number }[] = [
    { id: 'containers', label: 'Containers', icon: 'fa-solid fa-box', count: counts.containers },
    { id: 'images', label: 'Images', icon: 'fa-solid fa-cube', count: counts.images },
    { id: 'networks', label: 'Networks', icon: 'fa-solid fa-network-wired', count: counts.networks },
    { id: 'volumes', label: 'Volumes', icon: 'fa-solid fa-hard-drive', count: counts.volumes },
  ];

  // === Selected containers analysis ===
  const selectedAnalysis = useMemo(() => {
    const selected = containers.filter(c => selectedContainers.has(c.name));
    const updatable = selected.filter(c => c.has_update_data && isUpdatable(c.update_status || ''));
    const mismatches = selected.filter(c => c.has_update_data && isMismatch(c.update_status || ''));

    const patchCount = updatable.filter(c => c.change_type === ChangeType.PatchChange).length;
    const minorCount = updatable.filter(c => c.change_type === ChangeType.MinorChange).length;
    const majorCount = updatable.filter(c => c.change_type === ChangeType.MajorChange).length;
    const blockedCount = updatable.filter(c => c.update_status === 'UPDATE_AVAILABLE_BLOCKED').length;
    const rebuildCount = updatable.filter(c => (c.change_type === ChangeType.NoChange || c.change_type === ChangeType.UnknownChange) && c.update_status !== 'UPDATE_AVAILABLE_BLOCKED').length;

    return {
      count: selected.length,
      hasUpdates: updatable.length > 0,
      hasMismatches: mismatches.length > 0,
      hasRunning: selected.some(c => c.state === 'running'),
      hasStopped: selected.some(c => c.state !== 'running'),
      hasSelfUpdate: selected.some(c => c.name.toLowerCase().includes('docksmith') || c.image.toLowerCase().includes('docksmith')),
      hasBlocked: blockedCount > 0,
      patchCount,
      minorCount,
      majorCount,
      blockedCount,
      rebuildCount,
      mismatchCount: mismatches.length,
      updateTotal: updatable.length,
    };
  }, [containers, selectedContainers]);

  // === Loading state ===
  if (loading) {
    return (
      <div className="containers-page">
        <header className="containers-header">
          <div className="header-top">
            <h1>Containers</h1>
          </div>
          <SearchBar value="" onChange={() => {}} placeholder="Search..." disabled className="search-bar-skeleton" />
          <div className="explorer-toolbar">
            <div className="segmented-control segmented-control-sm explorer-tabs">
              {subTabs.map(t => (
                <button key={t.id} disabled className={activeSubTab === t.id ? 'active' : ''}>
                  <i className={t.icon}></i>
                  <span className="tab-label">{t.label}</span>
                  <span className="tab-count">-</span>
                </button>
              ))}
            </div>
          </div>
        </header>
        <main className="containers-content">
          {checkProgress ? (
            <div className="check-progress-overlay">
              <div className="check-progress">
                <div className="check-progress-bar">
                  <div className="check-progress-bar-fill" style={{ width: `${checkProgress.percent}%` }} />
                  <span className="check-progress-bar-text">{checkProgress.checked}/{checkProgress.total}</span>
                </div>
                <div className="check-progress-message">{checkProgress.message}</div>
              </div>
            </div>
          ) : (
            <SkeletonDashboard />
          )}
        </main>
      </div>
    );
  }

  if (error && !containers.length) {
    return (
      <div className="containers-page">
        <header className="containers-header"><h1>Containers</h1></header>
        <main className="containers-content">
          <div className="explorer-error">
            <i className="fa-solid fa-triangle-exclamation"></i>
            <p>{error}</p>
            <button onClick={cacheRefresh}>Retry</button>
          </div>
        </main>
      </div>
    );
  }

  return (
    <div
      className="containers-page"
      onTouchStart={handleTouchStart}
      onTouchMove={handleTouchMove}
      onTouchEnd={handleTouchEnd}
    >
      {/* Pull-to-refresh */}
      {pullDistance > 0 && (() => {
        const isBackground = pullDistance >= 70 && pullDistance < 120;
        const isCache = pullDistance >= 120;
        return (
          <div style={{
            position: 'absolute', top: 0, left: 0, right: 0, height: `${pullDistance}px`,
            display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
            background: isCache ? '#ff8c42' : isBackground ? '#4a9eff' : '#2c2c2c',
            transition: isPulling ? 'background 0.1s ease-out' : 'height 0.2s ease-out',
            zIndex: 1000,
          }}>
            <i className="fa-solid fa-rotate" style={{ fontSize: '1.5rem', color: isCache || isBackground ? '#fff' : '#888', transform: `rotate(${pullDistance * 3.6}deg)`, opacity: Math.min(pullDistance / 70, 1) }} />
            {pullDistance >= 40 && (
              <div style={{ marginTop: '8px', fontSize: '0.875rem', fontWeight: '500', color: isCache || isBackground ? '#fff' : '#888', opacity: Math.min(pullDistance / 70, 1) }}>
                {isCache ? 'Cache Refresh' : isBackground ? 'Background Refresh' : 'Pull to refresh'}
              </div>
            )}
          </div>
        );
      })()}

      <header className="containers-header">
        <div className="header-top">
          <h1>Containers</h1>
          {reconnecting && (
            <div className="connection-status reconnecting" title="Reconnecting...">
              <i className="fa-solid fa-wifi fa-fade"></i>
              <span>Reconnecting...</span>
            </div>
          )}
        </div>
        <SearchBar value={searchQuery} onChange={setSearchQuery} placeholder={`Search ${activeSubTab}...`} />

        {/* Resource sub-tabs toolbar */}
        <div className="explorer-toolbar">
          <div className="segmented-control segmented-control-sm explorer-tabs">
            {subTabs.map(t => (
              <button key={t.id} className={activeSubTab === t.id ? 'active' : ''} onClick={() => setActiveSubTab(t.id)}>
                <i className={t.icon}></i>
                <span className="tab-label">{t.label}</span>
                <span className="tab-count">{t.count}</span>
              </button>
            ))}
          </div>
          <div className="explorer-toolbar-actions">
            {activeSubTab !== 'containers' && (
              <>
                <div className="settings-menu-wrapper">
                  <button className={`explorer-settings-btn ${showSettings ? 'active' : ''}`} onClick={() => setShowSettings(!showSettings)} title="Settings">
                    <i className="fa-solid fa-gear"></i>
                  </button>
                  <ExplorerSettingsMenu
                    isOpen={showSettings}
                    onClose={() => setShowSettings(false)}
                    activeTab={activeSubTab as 'images' | 'networks' | 'volumes'}
                    settings={explorerSettings}
                    onSettingsChange={setExplorerSettings}
                    onReset={() => setExplorerSettings(DEFAULT_SETTINGS)}
                  />
                </div>
                <button className="prune-btn" onClick={() => setConfirmPrune(activeSubTab)} disabled={pruneLoading} title={`Prune unused ${activeSubTab}`}>
                  {pruneLoading ? <i className="fa-solid fa-circle-notch fa-spin"></i> : <i className="fa-solid fa-broom"></i>}
                  <span>Prune</span>
                </button>
              </>
            )}
          </div>
        </div>

        {/* Container-specific toolbar (only on containers sub-tab) */}
        {activeSubTab === 'containers' && (
          <div className="filter-toolbar">
            <div className="segmented-control">
              <button className={viewSettings.filter === 'all' ? 'active' : ''} onClick={() => updateViewSettings({ filter: 'all' })}>All</button>
              <button className={viewSettings.filter === 'updates' ? 'active' : ''} onClick={() => updateViewSettings({ filter: 'updates' })}>Updates</button>
            </div>
            <div className="toolbar-options">
              <button onClick={selectedContainers.size > 0 ? deselectAll : selectAll} className="icon-btn select-all-btn" title={selectedContainers.size > 0 ? 'Deselect all' : 'Select all'}>
                <i className={`fa-solid ${selectedContainers.size > 0 ? 'fa-square-minus' : 'fa-square-check'}`}></i>
              </button>
              {explorerSettings.containers.groupBy === 'stack' && (
                <button className="icon-btn" onClick={toggleAllStacks} title={collapsedStacks.size > 0 ? 'Expand all' : 'Collapse all'}>
                  <i className={`fa-solid ${collapsedStacks.size > 0 ? 'fa-chevron-right' : 'fa-chevron-down'}`}></i>
                </button>
              )}
              <div className="settings-menu-wrapper">
                <button className={`icon-btn dashboard-settings-btn ${showSettings ? 'active' : ''}`} onClick={() => setShowSettings(!showSettings)} title="View Options">
                  <i className="fa-solid fa-sliders"></i>
                </button>
                {showSettings && (
                  <>
                    <div className="settings-menu-backdrop" onClick={(e) => { e.stopPropagation(); setShowSettings(false); }} />
                    <div className="settings-menu" role="dialog" aria-label="View options">
                      <div className="settings-menu-header">
                        <span>View Options</span>
                        <button className="settings-close-btn" onClick={() => setShowSettings(false)}><i className="fa-solid fa-xmark"></i></button>
                      </div>
                      <div className="settings-menu-content">
                        <div className="settings-group">
                          <label className="settings-label">Group by</label>
                          <div className="settings-options">
                            {(['stack', 'status', 'image', 'network', 'none'] as const).map(opt => (
                              <button key={opt} className={explorerSettings.containers.groupBy === opt ? 'active' : ''} onClick={() => setExplorerSettings(s => ({ ...s, containers: { ...s.containers, groupBy: opt } }))}>
                                {opt === 'stack' && 'Stack'}{opt === 'status' && 'Status'}{opt === 'image' && 'Image'}{opt === 'network' && 'Network'}{opt === 'none' && 'None'}
                              </button>
                            ))}
                          </div>
                        </div>
                        <div className="settings-group">
                          <label className="settings-label">Sort by</label>
                          <div className="settings-options">
                            {(['name', 'status', 'created', 'image'] as const).map(opt => (
                              <button key={opt} className={explorerSettings.containers.sortBy === opt ? 'active' : ''} onClick={() => setExplorerSettings(s => ({ ...s, containers: { ...s.containers, sortBy: opt } }))}>
                                {opt === 'name' && 'Name'}{opt === 'status' && 'Status'}{opt === 'created' && 'Created'}{opt === 'image' && 'Image'}
                              </button>
                            ))}
                          </div>
                        </div>
                        <div className="settings-group">
                          <label className="settings-checkbox"><input type="checkbox" checked={viewSettings.showIgnored} onChange={(e) => updateViewSettings({ showIgnored: e.target.checked })} /><span>Show ignored</span></label>
                        </div>
                        <div className="settings-group">
                          <label className="settings-checkbox"><input type="checkbox" checked={viewSettings.showLocalImages} onChange={(e) => updateViewSettings({ showLocalImages: e.target.checked })} /><span>Show local images</span></label>
                        </div>
                        <div className="settings-group">
                          <label className="settings-checkbox"><input type="checkbox" checked={viewSettings.showMismatch} onChange={(e) => updateViewSettings({ showMismatch: e.target.checked })} /><span>Show mismatched</span></label>
                        </div>
                        <div className="settings-group">
                          <label className="settings-checkbox"><input type="checkbox" checked={viewSettings.showStandalone} onChange={(e) => updateViewSettings({ showStandalone: e.target.checked })} /><span>Show standalone</span></label>
                        </div>
                        <div className="settings-group">
                          <label className="settings-checkbox"><input type="checkbox" checked={explorerSettings.containers.showRunningOnly} onChange={(e) => setExplorerSettings(s => ({ ...s, containers: { ...s.containers, showRunningOnly: e.target.checked } }))} /><span>Running only</span></label>
                        </div>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </div>
          </div>
        )}
      </header>

      {/* Prune confirmation */}
      {confirmPrune && (
        <div className="confirm-dialog-overlay" onClick={cancelPrune}>
          <div className="confirm-dialog" ref={pruneDialogRef} role="dialog" aria-modal="true" onClick={e => e.stopPropagation()}>
            <div className="confirm-dialog-header"><h3>Confirm Prune</h3></div>
            <div className="confirm-dialog-body">
              <p>
                {confirmPrune === 'containers' && 'Remove all stopped containers?'}
                {confirmPrune === 'images' && 'Remove all dangling (unused) images?'}
                {confirmPrune === 'networks' && 'Remove all unused networks?'}
                {confirmPrune === 'volumes' && 'Remove all unused volumes?'}
              </p>
              <p className="confirm-warning">
                {confirmPrune === 'volumes' ? <><i className="fa-solid fa-triangle-exclamation"></i> This will permanently delete volume data!</> : 'This action cannot be undone.'}
              </p>
            </div>
            <div className="confirm-dialog-actions">
              <button className="confirm-cancel" onClick={cancelPrune}>Cancel</button>
              <button className="confirm-proceed confirm-danger" onClick={() => handlePrune(confirmPrune)} disabled={pruneLoading}>{pruneLoading ? 'Pruning...' : 'Prune'}</button>
            </div>
          </div>
        </div>
      )}

      <main className={`containers-content ${selectedContainers.size > 0 ? 'has-selection' : ''}`} ref={mainRef}>
        {activeSubTab === 'containers' && renderContainersTab()}
        {activeSubTab === 'images' && renderImagesTab()}
        {activeSubTab === 'networks' && renderNetworksTab()}
        {activeSubTab === 'volumes' && renderVolumesTab()}
      </main>

      {/* Selection bar */}
      {selectedContainers.size > 0 && (
        <div className={`selection-bar ${selectedAnalysis.hasBlocked ? 'has-force' : ''}`}>
          {/* Blocked containers excluded banner */}
          {blockedExcluded && (
            <div className="blocked-excluded-info">
              <i className="fa-solid fa-circle-info"></i>
              <span>{containers.filter(filterContainer).filter(c => c.update_status === 'UPDATE_AVAILABLE_BLOCKED').length} blocked container(s) excluded</span>
              <button className="include-blocked-btn" onClick={includeBlocked}>Include anyway</button>
            </div>
          )}

          {/* Force update warning when blocked containers are selected */}
          {selectedAnalysis.hasBlocked && (
            <div className="force-update-warning">
              <i className="fa-solid fa-triangle-exclamation"></i>
              <span>{selectedAnalysis.blockedCount} container(s) will skip pre-update checks</span>
            </div>
          )}

          {selectedAnalysis.hasSelfUpdate && (
            <div className="self-update-warning"><i className="fa-solid fa-triangle-exclamation"></i><span>Docksmith will restart to apply the update</span></div>
          )}
          <div className="selection-actions">
            <div className="selection-summary">
              <span className="selection-count">{selectedAnalysis.count} selected</span>
              {(selectedAnalysis.hasUpdates || selectedAnalysis.hasMismatches) && (
                <div className="update-type-badges">
                  {selectedAnalysis.patchCount > 0 && <span className="update-type-badge patch">{selectedAnalysis.patchCount} Patch</span>}
                  {selectedAnalysis.minorCount > 0 && <span className="update-type-badge minor">{selectedAnalysis.minorCount} Minor</span>}
                  {selectedAnalysis.majorCount > 0 && <span className="update-type-badge major">{selectedAnalysis.majorCount} Major</span>}
                  {selectedAnalysis.rebuildCount > 0 && <span className="update-type-badge rebuild">{selectedAnalysis.rebuildCount} Rebuild</span>}
                  {selectedAnalysis.blockedCount > 0 && <span className="update-type-badge blocked">{selectedAnalysis.blockedCount} Blocked</span>}
                  {selectedAnalysis.mismatchCount > 0 && <span className="update-type-badge mismatch">{selectedAnalysis.mismatchCount} Mismatch</span>}
                </div>
              )}
            </div>
            <div className="selection-buttons">
              <button className="cancel-btn" onClick={deselectAll}>Cancel</button>

              {/* Actions dropdown (secondary) */}
              <div className="bulk-actions-wrapper">
                <button className="actions-dropdown-btn" onClick={() => setShowBulkActions(!showBulkActions)}>
                  Actions <i className="fa-solid fa-chevron-down" style={{ fontSize: '0.75em', marginLeft: '4px' }}></i>
                </button>
                {showBulkActions && (
                  <>
                    {createPortal(
                      <div className="action-menu-backdrop" onClick={() => setShowBulkActions(false)} />,
                      document.body
                    )}
                    <div className="bulk-actions-menu">
                      {selectedAnalysis.hasStopped && (
                        <button onClick={() => handleBulkAction('start')}><i className="fa-solid fa-play"></i> Start</button>
                      )}
                      {selectedAnalysis.hasRunning && (
                        <button onClick={() => handleBulkAction('stop')}><i className="fa-solid fa-stop"></i> Stop</button>
                      )}
                      <button onClick={() => handleBulkAction('restart')}><i className="fa-solid fa-rotate"></i> Restart</button>
                      <button onClick={() => handleBulkAction('remove')} className="danger"><i className="fa-solid fa-trash"></i> Remove</button>
                      <div className="bulk-actions-divider" />
                      <span className="bulk-section-label">Labels</span>
                      <button onClick={() => handleBulkLabel({ ignore: true })}><i className="fa-solid fa-eye-slash"></i> Ignore</button>
                      <button onClick={() => handleBulkLabel({ ignore: false })}><i className="fa-solid fa-eye"></i> Unignore</button>
                      <button onClick={() => handleBulkLabel({ allow_latest: true })}><i className="fa-solid fa-tag"></i> Allow :latest</button>
                      <button onClick={() => handleBulkLabel({ allow_latest: false })}><i className="fa-solid fa-tag"></i> Disallow :latest</button>
                      <div className="bulk-actions-divider" />
                      <span className="bulk-section-label">Pinning</span>
                      <button onClick={() => handleBulkLabel({ version_pin_major: false, version_pin_minor: false, version_pin_patch: false })}><i className="fa-solid fa-thumbtack" style={{ opacity: 0.4 }}></i> Unpin</button>
                      <button onClick={() => handleBulkLabel({ version_pin_major: true, version_pin_minor: false, version_pin_patch: false })}><i className="fa-solid fa-thumbtack"></i> Pin Major</button>
                      <button onClick={() => handleBulkLabel({ version_pin_minor: true, version_pin_major: false, version_pin_patch: false })}><i className="fa-solid fa-thumbtack"></i> Pin Minor</button>
                      <button onClick={() => handleBulkLabel({ version_pin_patch: true, version_pin_major: false, version_pin_minor: false })}><i className="fa-solid fa-thumbtack"></i> Pin Patch</button>
                      <div className="bulk-actions-divider" />
                      <span className="bulk-section-label">Clear</span>
                      <button onClick={() => handleBulkLabel({ tag_regex: '' })}><i className="fa-solid fa-filter-circle-xmark"></i> Clear Tag Filter</button>
                      <button onClick={() => handleBulkLabel({ script: '' })}><i className="fa-solid fa-terminal"></i> Clear Script</button>
                    </div>
                  </>
                )}
              </div>

              {/* Update button - contextual */}
              {(selectedAnalysis.hasUpdates || selectedAnalysis.hasMismatches) && (
                <button className={`update-btn ${selectedAnalysis.hasBlocked ? 'force' : ''}`} onClick={handleBulkUpdate}>
                  {selectedAnalysis.hasBlocked && <i className="fa-solid fa-bolt"></i>}
                  {selectedAnalysis.hasBlocked ? 'Force Update' : 'Update'}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
