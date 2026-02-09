import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useFocusTrap } from '../hooks/useFocusTrap';
import {
  getExplorerData,
  removeImage,
  removeNetwork,
  removeVolume,
  pruneContainers,
  pruneImages,
  pruneNetworks,
  pruneVolumes,
} from '../api/client';
import type {
  ExplorerData,
  ContainerExplorerItem,
  ImageInfo,
  NetworkInfo,
  VolumeInfo,
} from '../types/api';
import { useToast } from './Toast';
import { SearchBar, StackGroup, ActionMenuButton, ActionMenu, ActionMenuItem } from './shared';
import { ContainerItem } from './Explorer/ContainerItem';
import { ImageItem } from './Explorer/ImageItem';
import { NetworkItem } from './Explorer/NetworkItem';
import { VolumeItem } from './Explorer/VolumeItem';
import {
  ExplorerSettingsMenu,
  DEFAULT_SETTINGS,
  type ExplorerSettings,
} from './Explorer/ExplorerSettings';

// Local storage keys
const STORAGE_KEY_ACTIVE_TAB = 'explorer_active_tab';
const STORAGE_KEY_COLLAPSED_STACKS = 'explorer_collapsed_stacks';
const STORAGE_KEY_SETTINGS = 'explorer_settings';

type ExplorerTab = 'containers' | 'images' | 'networks' | 'volumes';

interface ExplorerProps {
  onBack?: () => void;
}

export function Explorer({ onBack: _onBack }: ExplorerProps) {
  const navigate = useNavigate();
  const toast = useToast();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [data, setData] = useState<ExplorerData | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [activeTab, setActiveTab] = useState<ExplorerTab>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_ACTIVE_TAB);
    return (saved as ExplorerTab) || 'containers';
  });
  const [collapsedStacks, setCollapsedStacks] = useState<Set<string>>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_COLLAPSED_STACKS);
    return saved ? new Set(JSON.parse(saved)) : new Set();
  });
  const [activeActionMenu, setActiveActionMenu] = useState<string | null>(null);
  const [actionLoading] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  // State for image/network/volume action menus
  const [activeImageMenu, setActiveImageMenu] = useState<string | null>(null);
  const [activeNetworkMenu, setActiveNetworkMenu] = useState<string | null>(null);
  const [activeVolumeMenu, setActiveVolumeMenu] = useState<string | null>(null);
  const [confirmImageRemove, setConfirmImageRemove] = useState<string | null>(null);
  const [confirmNetworkRemove, setConfirmNetworkRemove] = useState<string | null>(null);
  const [confirmVolumeRemove, setConfirmVolumeRemove] = useState<string | null>(null);
  const [imageLoading, setImageLoading] = useState<string | null>(null);
  const [networkLoading, setNetworkLoading] = useState<string | null>(null);
  const [volumeLoading, setVolumeLoading] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  // Stack action menu state
  const [activeStackMenu, setActiveStackMenu] = useState<string | null>(null);
  const stackMenuRef = useRef<HTMLDivElement>(null);

  // Prune state
  const [pruneLoading, setPruneLoading] = useState(false);
  const [confirmPrune, setConfirmPrune] = useState<ExplorerTab | null>(null);
  const cancelPrune = useCallback(() => setConfirmPrune(null), []);
  const pruneDialogRef = useFocusTrap(!!confirmPrune, cancelPrune);

  // Settings state
  const [showSettings, setShowSettings] = useState(false);
  const [settings, setSettings] = useState<ExplorerSettings>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_SETTINGS);
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        // Deep merge to preserve defaults for new settings fields
        return {
          containers: { ...DEFAULT_SETTINGS.containers, ...parsed.containers },
          images: { ...DEFAULT_SETTINGS.images, ...parsed.images },
          networks: { ...DEFAULT_SETTINGS.networks, ...parsed.networks },
          volumes: { ...DEFAULT_SETTINGS.volumes, ...parsed.volumes },
        };
      } catch {
        return DEFAULT_SETTINGS;
      }
    }
    return DEFAULT_SETTINGS;
  });

  // Pull-to-refresh state
  const [pullDistance, setPullDistance] = useState(0);
  const [isPulling, setIsPulling] = useState(false);
  const pullStartY = useRef<number | null>(null);
  const contentRef = useRef<HTMLElement>(null);

  // Save active tab to localStorage
  useEffect(() => {
    localStorage.setItem(STORAGE_KEY_ACTIVE_TAB, activeTab);
  }, [activeTab]);

  // Save collapsed stacks to localStorage
  useEffect(() => {
    localStorage.setItem(STORAGE_KEY_COLLAPSED_STACKS, JSON.stringify([...collapsedStacks]));
  }, [collapsedStacks]);

  // Save settings to localStorage
  useEffect(() => {
    localStorage.setItem(STORAGE_KEY_SETTINGS, JSON.stringify(settings));
  }, [settings]);

  const handleResetSettings = () => {
    setSettings(DEFAULT_SETTINGS);
  };

  // Fetch explorer data
  const fetchData = useCallback(async () => {
    try {
      const response = await getExplorerData();
      if (response.success && response.data) {
        setData(response.data);
        setError(null);
      } else {
        setError(response.error || 'Failed to fetch data');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Close menu when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setActiveActionMenu(null);
        setConfirmRemove(null);
        setActiveImageMenu(null);
        setActiveNetworkMenu(null);
        setActiveVolumeMenu(null);
        setConfirmImageRemove(null);
        setConfirmNetworkRemove(null);
        setConfirmVolumeRemove(null);
      }
      if (stackMenuRef.current && !stackMenuRef.current.contains(event.target as Node)) {
        setActiveStackMenu(null);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const toggleStack = (stackName: string) => {
    setCollapsedStacks(prev => {
      const next = new Set(prev);
      if (next.has(stackName)) {
        next.delete(stackName);
      } else {
        next.add(stackName);
      }
      return next;
    });
  };

  // Pull-to-refresh handlers
  const handleTouchStart = (e: React.TouchEvent) => {
    // Only allow pull-to-refresh when scrolled to the top of the content
    const scrollTop = contentRef.current?.scrollTop ?? 0;
    if (scrollTop === 0 && !loading) {
      pullStartY.current = e.touches[0].clientY;
      setIsPulling(true);
    }
  };

  const handleTouchMove = (e: React.TouchEvent) => {
    if (!isPulling || pullStartY.current === null) return;

    const currentY = e.touches[0].clientY;
    const distance = currentY - pullStartY.current;

    // Only track downward pulls
    if (distance > 0) {
      setPullDistance(Math.min(distance, 100)); // Cap at 100px
    }
  };

  const handleTouchEnd = () => {
    // Refresh threshold: 70px
    if (pullDistance >= 70) {
      toast.info('Refreshing...');
      fetchData();
    }

    // Reset pull state
    setIsPulling(false);
    setPullDistance(0);
    pullStartY.current = null;
  };

  // Container actions — all navigate to the operation page
  const handleContainerAction = async (
    containerName: string,
    action: 'start' | 'stop' | 'restart' | 'remove',
    force?: boolean
  ) => {
    setActiveActionMenu(null);
    setConfirmRemove(null);
    navigate('/operation', {
      state: { [action]: { containerName, force } }
    });
  };

  // Stack actions - restart or stop all containers in a stack
  // Both navigate to the operation progress page for proper tracking and history
  const handleStackAction = (
    stackName: string,
    action: 'restart' | 'stop'
  ) => {
    const stackContainers = containerGroups[stackName];
    if (!stackContainers || stackContainers.length === 0) return;

    setActiveStackMenu(null);

    if (action === 'restart') {
      navigate('/operation', {
        state: {
          stackRestart: {
            stackName,
            containers: stackContainers.map(c => c.name),
          }
        }
      });
    } else {
      navigate('/operation', {
        state: {
          stackStop: {
            stackName,
            containers: stackContainers.map(c => c.name),
          }
        }
      });
    }
  };

  // Image actions
  const handleImageRemove = async (imageId: string, force?: boolean) => {
    setImageLoading(imageId);
    setActiveImageMenu(null);
    setConfirmImageRemove(null);

    try {
      const result = await removeImage(imageId, { force });
      if (result.success) {
        toast.success('Image removed successfully');
        await fetchData();
      } else {
        toast.error(result.error || 'Failed to remove image');
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to remove image');
    } finally {
      setImageLoading(null);
    }
  };

  // Network actions
  const handleNetworkRemove = async (networkId: string) => {
    setNetworkLoading(networkId);
    setActiveNetworkMenu(null);
    setConfirmNetworkRemove(null);

    try {
      const result = await removeNetwork(networkId);
      if (result.success) {
        toast.success('Network removed successfully');
        await fetchData();
      } else {
        toast.error(result.error || 'Failed to remove network');
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to remove network');
    } finally {
      setNetworkLoading(null);
    }
  };

  // Volume actions
  const handleVolumeRemove = async (volumeName: string, force?: boolean) => {
    setVolumeLoading(volumeName);
    setActiveVolumeMenu(null);
    setConfirmVolumeRemove(null);

    try {
      const result = await removeVolume(volumeName, { force });
      if (result.success) {
        toast.success('Volume removed successfully');
        await fetchData();
      } else {
        toast.error(result.error || 'Failed to remove volume');
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to remove volume');
    } finally {
      setVolumeLoading(null);
    }
  };

  // Prune actions
  const handlePrune = async (tab: ExplorerTab) => {
    setPruneLoading(true);
    setConfirmPrune(null);

    try {
      let result;
      switch (tab) {
        case 'containers':
          result = await pruneContainers();
          break;
        case 'images':
          result = await pruneImages();
          break;
        case 'networks':
          result = await pruneNetworks();
          break;
        case 'volumes':
          result = await pruneVolumes();
          break;
      }

      if (result.success && result.data) {
        const count = result.data.items_deleted?.length || 0;
        const space = result.data.space_reclaimed;
        let message = result.data.message || `Pruned ${count} ${tab}`;
        if (space && space > 0) {
          message += ` (${formatBytes(space)} reclaimed)`;
        }
        toast.success(message);
        await fetchData();
      } else {
        toast.error(result.error || `Failed to prune ${tab}`);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Failed to prune ${tab}`);
    } finally {
      setPruneLoading(false);
    }
  };

  // Format bytes to human readable
  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  };

  // Sorting functions
  const sortContainers = (containers: ContainerExplorerItem[]): ContainerExplorerItem[] => {
    const sorted = [...containers];
    const asc = settings.containers.ascending;
    let result: ContainerExplorerItem[];
    switch (settings.containers.sortBy) {
      case 'name':
        result = sorted.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case 'status':
        // Sort by state, then health_status, then name
        result = sorted.sort((a, b) => {
          const stateOrder: Record<string, number> = { running: 0, paused: 1, restarting: 2, exited: 3, dead: 4 };
          const aOrder = stateOrder[a.state] ?? 5;
          const bOrder = stateOrder[b.state] ?? 5;
          if (aOrder !== bOrder) return aOrder - bOrder;
          return a.name.localeCompare(b.name);
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
  };

  const sortImages = (images: ImageInfo[]): ImageInfo[] => {
    const sorted = [...images];
    const asc = settings.images.ascending;
    let result: ImageInfo[];
    switch (settings.images.sortBy) {
      case 'tags':
        result = sorted.sort((a, b) => {
          const aTag = a.tags?.[0] || '<none>';
          const bTag = b.tags?.[0] || '<none>';
          return aTag.localeCompare(bTag);
        });
        break;
      case 'size':
        result = sorted.sort((a, b) => b.size - a.size);
        break;
      case 'created':
        result = sorted.sort((a, b) => b.created - a.created);
        break;
      case 'id':
        result = sorted.sort((a, b) => a.id.localeCompare(b.id));
        break;
      default:
        result = sorted;
    }
    return asc ? result : result.reverse();
  };

  const sortNetworks = (networks: NetworkInfo[]): NetworkInfo[] => {
    const sorted = [...networks];
    const asc = settings.networks.ascending;
    let result: NetworkInfo[];
    switch (settings.networks.sortBy) {
      case 'name':
        result = sorted.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case 'driver':
        result = sorted.sort((a, b) => a.driver.localeCompare(b.driver) || a.name.localeCompare(b.name));
        break;
      case 'containers':
        result = sorted.sort((a, b) => (b.containers?.length || 0) - (a.containers?.length || 0));
        break;
      case 'created':
        result = sorted.sort((a, b) => b.created - a.created);
        break;
      default:
        result = sorted;
    }
    return asc ? result : result.reverse();
  };

  const sortVolumes = (volumes: VolumeInfo[]): VolumeInfo[] => {
    const sorted = [...volumes];
    const asc = settings.volumes.ascending;
    let result: VolumeInfo[];
    switch (settings.volumes.sortBy) {
      case 'name':
        result = sorted.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case 'size':
        result = sorted.sort((a, b) => (b.size || 0) - (a.size || 0));
        break;
      case 'created':
        result = sorted.sort((a, b) => b.created - a.created);
        break;
      case 'driver':
        result = sorted.sort((a, b) => a.driver.localeCompare(b.driver) || a.name.localeCompare(b.name));
        break;
      default:
        result = sorted;
    }
    return asc ? result : result.reverse();
  };

  // Filter items based on search query
  const filterItems = <T extends { name?: string; tags?: string[] }>(
    items: T[],
    query: string,
    getSearchableText: (item: T) => string
  ): T[] => {
    if (!query) return items;
    const lowerQuery = query.toLowerCase();
    return items.filter(item => getSearchableText(item).toLowerCase().includes(lowerQuery));
  };

  // Memoize counts for tabs
  const counts = useMemo(() => {
    if (!data) return { containers: 0, images: 0, networks: 0, volumes: 0 };
    const containerCount =
      Object.values(data.container_stacks || {}).reduce((acc, arr) => acc + arr.length, 0) +
      (data.standalone_containers || []).length;
    return {
      containers: containerCount,
      images: data.images?.length || 0,
      networks: data.networks?.length || 0,
      volumes: data.volumes?.length || 0,
    };
  }, [data]);

  // Memoize container groups - expensive filtering/sorting/grouping
  const containerGroups = useMemo(() => {
    if (!data) return {};

    const groups: Record<string, ContainerExplorerItem[]> = {};
    let allContainers: ContainerExplorerItem[] = [
      ...Object.values(data.container_stacks || {}).flat(),
      ...(data.standalone_containers || []),
    ];

    // Apply running only filter
    if (settings.containers.showRunningOnly) {
      allContainers = allContainers.filter(c => c.state === 'running');
    }

    const filteredAllContainers = sortContainers(filterItems(
      allContainers,
      searchQuery,
      c => `${c.name} ${c.image} ${c.stack || ''}`
    ));

    if (settings.containers.groupBy === 'none') {
      if (filteredAllContainers.length > 0) {
        groups['All Containers'] = filteredAllContainers;
      }
    } else if (settings.containers.groupBy === 'status') {
      for (const container of filteredAllContainers) {
        const state = container.state || 'unknown';
        const groupName = state.charAt(0).toUpperCase() + state.slice(1);
        if (!groups[groupName]) {
          groups[groupName] = [];
        }
        groups[groupName].push(container);
      }
    } else if (settings.containers.groupBy === 'image') {
      // Group by image name (without tag)
      for (const container of filteredAllContainers) {
        const imageName = container.image.split(':')[0] || container.image;
        if (!groups[imageName]) {
          groups[imageName] = [];
        }
        groups[imageName].push(container);
      }
    } else if (settings.containers.groupBy === 'network') {
      // Group by first network (containers can have multiple)
      for (const container of filteredAllContainers) {
        const network = container.networks?.[0] || 'none';
        if (!groups[network]) {
          groups[network] = [];
        }
        groups[network].push(container);
      }
    } else {
      // Group by stack (default)
      const stackNames = Object.keys(data.container_stacks || {}).sort();
      for (const stackName of stackNames) {
        let stackContainers = data.container_stacks[stackName] || [];
        if (settings.containers.showRunningOnly) {
          stackContainers = stackContainers.filter(c => c.state === 'running');
        }
        const filtered = sortContainers(filterItems(
          stackContainers,
          searchQuery,
          c => `${c.name} ${c.image} ${stackName}`
        ));
        if (filtered.length > 0) {
          groups[stackName] = filtered;
        }
      }
      let standalone = data.standalone_containers || [];
      if (settings.containers.showRunningOnly) {
        standalone = standalone.filter(c => c.state === 'running');
      }
      const filteredStandalone = sortContainers(filterItems(
        standalone,
        searchQuery,
        c => `${c.name} ${c.image}`
      ));
      if (filteredStandalone.length > 0) {
        groups['_standalone'] = filteredStandalone;
      }
    }

    return groups;
  }, [data, searchQuery, settings.containers.groupBy, settings.containers.sortBy, settings.containers.ascending, settings.containers.showRunningOnly]);

  // Helper to extract repository from image tag
  const getRepository = (image: ImageInfo): string => {
    const tag = image.tags?.[0];
    if (!tag || tag === '<none>') return '<none>';
    // Split by : to remove tag, then get the repo part
    const withoutTag = tag.split(':')[0];
    return withoutTag || '<none>';
  };

  // Memoize image groups - supports grouping by repository
  const imageGroups = useMemo(() => {
    if (!data) return {};

    let images = filterItems(
      data.images || [],
      searchQuery,
      i => (i.tags || []).join(' ')
    );
    if (settings.images.showDanglingOnly) {
      images = images.filter(i => i.dangling);
    }
    const sorted = sortImages(images);

    if (settings.images.groupBy === 'none') {
      return { 'All Images': sorted };
    }

    // Group by repository, with dangling images in their own group
    const groups: Record<string, ImageInfo[]> = {};
    for (const image of sorted) {
      const group = image.dangling ? 'Dangling' : getRepository(image);
      if (!groups[group]) {
        groups[group] = [];
      }
      groups[group].push(image);
    }
    return groups;
  }, [data, searchQuery, settings.images.showDanglingOnly, settings.images.sortBy, settings.images.ascending, settings.images.groupBy]);

  // Memoize filtered images (flat list for backward compatibility)
  const filteredImages = useMemo(() => {
    return Object.values(imageGroups).flat();
  }, [imageGroups]);

  // Memoize network groups - supports grouping by driver
  const networkGroups = useMemo(() => {
    if (!data) return {};

    let networks = filterItems(
      data.networks || [],
      searchQuery,
      n => `${n.name} ${n.driver}`
    );

    // Apply hideBuiltIn filter
    if (settings.networks.hideBuiltIn) {
      networks = networks.filter(n => !n.is_default);
    }

    const sorted = sortNetworks(networks);

    if (settings.networks.groupBy === 'none') {
      return { 'All Networks': sorted };
    }

    // Group by driver
    const groups: Record<string, NetworkInfo[]> = {};
    for (const network of sorted) {
      const driver = network.driver || 'unknown';
      if (!groups[driver]) {
        groups[driver] = [];
      }
      groups[driver].push(network);
    }
    return groups;
  }, [data, searchQuery, settings.networks.sortBy, settings.networks.ascending, settings.networks.groupBy, settings.networks.hideBuiltIn]);

  // Memoize filtered networks (flat list for backward compatibility)
  const filteredNetworks = useMemo(() => {
    return Object.values(networkGroups).flat();
  }, [networkGroups]);

  // Memoize volume groups - supports grouping by type (Named vs Anonymous)
  const volumeGroups = useMemo(() => {
    if (!data) return {};
    let volumes = filterItems(
      data.volumes || [],
      searchQuery,
      v => `${v.name} ${v.driver} ${(v.containers || []).join(' ')}`
    );
    if (settings.volumes.showUnusedOnly) {
      volumes = volumes.filter(v => !v.containers || v.containers.length === 0);
    }
    const sorted = sortVolumes(volumes);

    if (settings.volumes.groupBy === 'none') {
      return { 'All Volumes': sorted };
    }

    // Group by type: Named vs Anonymous (SHA-256 hashes)
    const groups: Record<string, VolumeInfo[]> = {};
    for (const volume of sorted) {
      const isAnon = /^[0-9a-f]{64}$/.test(volume.name);
      const group = isAnon ? 'Anonymous' : 'Named';
      if (!groups[group]) groups[group] = [];
      groups[group].push(volume);
    }
    return groups;
  }, [data, searchQuery, settings.volumes.showUnusedOnly, settings.volumes.sortBy, settings.volumes.ascending, settings.volumes.groupBy]);

  // Memoize filtered volumes (flat list for backward compatibility)
  const filteredVolumes = useMemo(() => {
    return Object.values(volumeGroups).flat();
  }, [volumeGroups]);

  if (loading) {
    return (
      <div className="explorer">
        <header className="explorer-header">
          <h1>Explorer</h1>
          <SearchBar
            value=""
            onChange={() => {}}
            placeholder={`Search ${activeTab}...`}
            disabled
            className="explorer-search"
          />
          <div className="explorer-toolbar">
            <div className="segmented-control segmented-control-sm explorer-tabs">
              <button disabled className={activeTab === 'containers' ? 'active' : ''}>
                <i className="fa-solid fa-box"></i>
                <span className="tab-label">Containers</span>
                <span className="tab-count">-</span>
              </button>
              <button disabled className={activeTab === 'images' ? 'active' : ''}>
                <i className="fa-solid fa-cube"></i>
                <span className="tab-label">Images</span>
                <span className="tab-count">-</span>
              </button>
              <button disabled className={activeTab === 'networks' ? 'active' : ''}>
                <i className="fa-solid fa-network-wired"></i>
                <span className="tab-label">Networks</span>
                <span className="tab-count">-</span>
              </button>
              <button disabled className={activeTab === 'volumes' ? 'active' : ''}>
                <i className="fa-solid fa-hard-drive"></i>
                <span className="tab-label">Volumes</span>
                <span className="tab-count">-</span>
              </button>
            </div>
            <div className="explorer-toolbar-actions">
              <button className="explorer-settings-btn" disabled>
                <i className="fa-solid fa-gear"></i>
              </button>
              <button className="prune-btn" disabled>
                <i className="fa-solid fa-broom"></i>
                <span>Prune</span>
              </button>
            </div>
          </div>
        </header>
        <main className="explorer-content">
          <div className="explorer-loading">
            <i className="fa-solid fa-circle-notch fa-spin"></i>
          </div>
        </main>
      </div>
    );
  }

  if (error) {
    return (
      <div className="explorer">
        <header className="explorer-header">
          <h1>Explorer</h1>
        </header>
        <main className="explorer-content">
          <div className="explorer-error">
            <i className="fa-solid fa-triangle-exclamation"></i>
            <p>{error}</p>
            <button onClick={() => window.location.reload()}>Retry</button>
          </div>
        </main>
      </div>
    );
  }

  if (!data) return null;

  // Get the appropriate icon for container groups
  const getGroupIcon = (groupName: string): string => {
    if (settings.containers.groupBy === 'status') {
      // Status-based icons
      const statusIcons: Record<string, string> = {
        Running: 'fa-circle-play',
        Paused: 'fa-circle-pause',
        Restarting: 'fa-rotate',
        Exited: 'fa-circle-stop',
        Dead: 'fa-skull',
        Created: 'fa-circle-plus',
      };
      return statusIcons[groupName] || 'fa-box';
    }
    if (settings.containers.groupBy === 'none') {
      return 'fa-boxes-stacked';
    }
    // Stack grouping
    return groupName === '_standalone' ? 'fa-box' : 'fa-layer-group';
  };

  const renderContainersTab = () => {
    const groupEntries = Object.entries(containerGroups);

    // Sort groups
    const sortedGroups = settings.containers.groupBy === 'status'
      ? groupEntries.sort(([a], [b]) => {
          // Custom sort order for status groups
          const order = ['Running', 'Paused', 'Restarting', 'Created', 'Exited', 'Dead'];
          return order.indexOf(a) - order.indexOf(b);
        })
      : settings.containers.groupBy === 'stack'
        ? groupEntries.sort(([a], [b]) => {
            // Standalone at the end
            if (a === '_standalone') return 1;
            if (b === '_standalone') return -1;
            return a.localeCompare(b);
          })
        : groupEntries;

    // Check if any container in the stack is running
    const hasRunningContainers = (containers: ContainerExplorerItem[]) =>
      containers.some(c => c.state === 'running');

    // Render stack action menu - only for stack grouping
    const renderStackActions = (stackName: string, containers: ContainerExplorerItem[]) => {
      if (settings.containers.groupBy !== 'stack') return null;
      if (stackName === '_standalone') return null; // No stack actions for standalone containers

      const isActive = activeStackMenu === stackName;
      const hasRunning = hasRunningContainers(containers);

      return (
        <div
          className="stack-actions"
          ref={isActive ? stackMenuRef : undefined}
          onClick={(e) => e.stopPropagation()}
        >
          <ActionMenuButton
            isActive={isActive}
            isLoading={false}
            onClick={(e) => {
              e.stopPropagation();
              setActiveStackMenu(isActive ? null : stackName);
            }}
          />
          <ActionMenu isActive={isActive}>
            {hasRunning && (
              <>
                <ActionMenuItem
                  icon="fa-stop"
                  label="Stop Stack"
                  onClick={() => handleStackAction(stackName, 'stop')}
                />
                <ActionMenuItem
                  icon="fa-rotate"
                  label="Restart Stack"
                  onClick={() => handleStackAction(stackName, 'restart')}
                />
              </>
            )}
            {!hasRunning && (
              <ActionMenuItem
                icon="fa-rotate"
                label="Restart Stack"
                onClick={() => handleStackAction(stackName, 'restart')}
                title="Start all containers in the stack"
              />
            )}
          </ActionMenu>
        </div>
      );
    };

    return (
      <div className="explorer-list-container">
        {sortedGroups.map(([groupName, containers]) => (
          <StackGroup
            key={groupName}
            name={groupName === '_standalone' ? 'Standalone' : groupName}
            isCollapsed={collapsedStacks.has(groupName)}
            onToggle={() => toggleStack(groupName)}
            count={containers.length}
            icon={getGroupIcon(groupName)}
            isStandalone={groupName === '_standalone' || settings.containers.groupBy === 'none'}
            listClassName="explorer-list"
            actions={renderStackActions(groupName, containers)}
          >
            {containers.map(container => (
              <ContainerItem
                key={container.id}
                container={container}
                isActive={activeActionMenu === container.name}
                isLoading={actionLoading === container.name}
                confirmRemove={confirmRemove === container.name}
                showLabels={settings.containers.showLabels}
                onMenuToggle={() => {
                  setActiveActionMenu(activeActionMenu === container.name ? null : container.name);
                  setConfirmRemove(null);
                }}
                onAction={handleContainerAction}
                onNavigateToDetail={(tab?: 'overview' | 'logs' | 'inspect') => navigate(`/explorer/container/${container.name}`, { state: { tab } })}
                onConfirmRemove={() => setConfirmRemove(container.name)}
                menuRef={menuRef}
              />
            ))}
          </StackGroup>
        ))}

        {groupEntries.length === 0 && (
          <div className="explorer-empty">No containers found</div>
        )}
      </div>
    );
  };

  const renderImagesTab = () => {
    const groupEntries = Object.entries(imageGroups).sort(([a], [b]) => {
      // Dangling group always at end
      if (a === 'Dangling') return 1;
      if (b === 'Dangling') return -1;
      return a.localeCompare(b);
    });
    const showGroups = settings.images.groupBy !== 'none' && groupEntries.length > 0;

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {filteredImages.map(image => (
            <ImageItem
              key={image.id}
              image={image}
              isActive={activeImageMenu === image.id}
              isLoading={imageLoading === image.id}
              confirmRemove={confirmImageRemove === image.id}
              onMenuToggle={() => {
                setActiveImageMenu(activeImageMenu === image.id ? null : image.id);
                setConfirmImageRemove(null);
              }}
              onRemove={(force?: boolean) => handleImageRemove(image.id, force)}
              onConfirmRemove={() => setConfirmImageRemove(image.id)}
              menuRef={menuRef}
            />
          ))}
          {filteredImages.length === 0 && (
            <li className="explorer-empty">No images found</li>
          )}
        </ul>
      );
    }

    return (
      <div className="explorer-list-container">
        {groupEntries.map(([groupName, images]) => (
          <StackGroup
            key={groupName}
            name={groupName}
            isCollapsed={collapsedStacks.has(`img_${groupName}`)}
            onToggle={() => toggleStack(`img_${groupName}`)}
            count={images.length}
            icon={groupName === 'Dangling' ? 'fa-ghost' : 'fa-cube'}
            isStandalone={false}
            listClassName="explorer-list"
          >
            {images.map(image => (
              <ImageItem
                key={image.id}
                image={image}
                isActive={activeImageMenu === image.id}
                isLoading={imageLoading === image.id}
                confirmRemove={confirmImageRemove === image.id}
                onMenuToggle={() => {
                  setActiveImageMenu(activeImageMenu === image.id ? null : image.id);
                  setConfirmImageRemove(null);
                }}
                onRemove={(force?: boolean) => handleImageRemove(image.id, force)}
                onConfirmRemove={() => setConfirmImageRemove(image.id)}
                menuRef={menuRef}
              />
            ))}
          </StackGroup>
        ))}
        {groupEntries.length === 0 && (
          <div className="explorer-empty">No images found</div>
        )}
      </div>
    );
  };

  const renderNetworksTab = () => {
    const groupEntries = Object.entries(networkGroups).sort(([a], [b]) => a.localeCompare(b));
    const showGroups = settings.networks.groupBy !== 'none' && groupEntries.length > 0;

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {filteredNetworks.map(network => (
            <NetworkItem
              key={network.id}
              network={network}
              isActive={activeNetworkMenu === network.id}
              isLoading={networkLoading === network.id}
              confirmRemove={confirmNetworkRemove === network.id}
              onMenuToggle={() => {
                setActiveNetworkMenu(activeNetworkMenu === network.id ? null : network.id);
                setConfirmNetworkRemove(null);
              }}
              onRemove={() => handleNetworkRemove(network.id)}
              onConfirmRemove={() => setConfirmNetworkRemove(network.id)}
              menuRef={menuRef}
            />
          ))}
          {filteredNetworks.length === 0 && (
            <li className="explorer-empty">No networks found</li>
          )}
        </ul>
      );
    }

    return (
      <div className="explorer-list-container">
        {groupEntries.map(([groupName, networks]) => (
          <StackGroup
            key={groupName}
            name={groupName}
            isCollapsed={collapsedStacks.has(`net_${groupName}`)}
            onToggle={() => toggleStack(`net_${groupName}`)}
            count={networks.length}
            icon="fa-network-wired"
            isStandalone={false}
            listClassName="explorer-list"
          >
            {networks.map(network => (
              <NetworkItem
                key={network.id}
                network={network}
                isActive={activeNetworkMenu === network.id}
                isLoading={networkLoading === network.id}
                confirmRemove={confirmNetworkRemove === network.id}
                onMenuToggle={() => {
                  setActiveNetworkMenu(activeNetworkMenu === network.id ? null : network.id);
                  setConfirmNetworkRemove(null);
                }}
                onRemove={() => handleNetworkRemove(network.id)}
                onConfirmRemove={() => setConfirmNetworkRemove(network.id)}
                menuRef={menuRef}
              />
            ))}
          </StackGroup>
        ))}
        {groupEntries.length === 0 && (
          <div className="explorer-empty">No networks found</div>
        )}
      </div>
    );
  };

  const renderVolumeItem = (volume: VolumeInfo) => (
    <VolumeItem
      key={volume.name}
      volume={volume}
      isActive={activeVolumeMenu === volume.name}
      isLoading={volumeLoading === volume.name}
      confirmRemove={confirmVolumeRemove === volume.name}
      onMenuToggle={() => {
        setActiveVolumeMenu(activeVolumeMenu === volume.name ? null : volume.name);
        setConfirmVolumeRemove(null);
      }}
      onRemove={(force?: boolean) => handleVolumeRemove(volume.name, force)}
      onConfirmRemove={() => setConfirmVolumeRemove(volume.name)}
      menuRef={menuRef}
    />
  );

  const renderVolumesTab = () => {
    const groupEntries = Object.entries(volumeGroups);
    // Sort: Named first, Anonymous second
    groupEntries.sort(([a], [b]) => {
      if (a === 'Named') return -1;
      if (b === 'Named') return 1;
      return a.localeCompare(b);
    });
    const showGroups = settings.volumes.groupBy !== 'none' && groupEntries.length > 0;

    if (!showGroups) {
      return (
        <ul className="explorer-list">
          {filteredVolumes.map(renderVolumeItem)}
          {filteredVolumes.length === 0 && (
            <li className="explorer-empty">No volumes found</li>
          )}
        </ul>
      );
    }

    return (
      <div className="explorer-list-container">
        {groupEntries.map(([groupName, volumes]) => (
          <StackGroup
            key={groupName}
            name={groupName}
            isCollapsed={collapsedStacks.has(`vol_${groupName}`)}
            onToggle={() => toggleStack(`vol_${groupName}`)}
            count={volumes.length}
            icon={groupName === 'Anonymous' ? 'fa-fingerprint' : 'fa-hard-drive'}
            isStandalone={false}
            listClassName="explorer-list"
          >
            {volumes.map(renderVolumeItem)}
          </StackGroup>
        ))}
        {groupEntries.length === 0 && (
          <div className="explorer-empty">No volumes found</div>
        )}
      </div>
    );
  };

  const tabs: { id: ExplorerTab; label: string; icon: string; count: number }[] = [
    { id: 'containers', label: 'Containers', icon: 'fa-solid fa-box', count: counts.containers },
    { id: 'images', label: 'Images', icon: 'fa-solid fa-cube', count: counts.images },
    { id: 'networks', label: 'Networks', icon: 'fa-solid fa-network-wired', count: counts.networks },
    { id: 'volumes', label: 'Volumes', icon: 'fa-solid fa-hard-drive', count: counts.volumes },
  ];

  return (
    <div
      className="explorer"
      onTouchStart={handleTouchStart}
      onTouchMove={handleTouchMove}
      onTouchEnd={handleTouchEnd}
    >
      {/* Pull-to-refresh indicator */}
      {pullDistance > 0 && (
        <div
          className="pull-refresh-indicator"
          style={{
            height: `${pullDistance}px`,
            background: pullDistance >= 70 ? '#4a9eff' : '#2c2c2c',
          }}
        >
          <i
            className="fa-solid fa-rotate"
            style={{
              fontSize: '1.5rem',
              color: pullDistance >= 70 ? '#ffffff' : '#888888',
              transform: `rotate(${pullDistance * 3.6}deg)`,
              opacity: Math.min(pullDistance / 70, 1),
            }}
          />
          <span
            style={{
              fontSize: '0.75rem',
              color: pullDistance >= 70 ? '#ffffff' : '#888888',
              marginTop: '4px',
              opacity: Math.min(pullDistance / 50, 1),
            }}
          >
            {pullDistance >= 70 ? 'Release to refresh' : 'Pull to refresh'}
          </span>
        </div>
      )}

      <header className="explorer-header">
        <h1>Explorer</h1>
        <SearchBar
          value={searchQuery}
          onChange={setSearchQuery}
          placeholder={`Search ${activeTab}...`}
          className="explorer-search"
        />
        <div className="explorer-toolbar">
          <div className="segmented-control segmented-control-sm explorer-tabs">
            {tabs.map(tab => (
              <button
                key={tab.id}
                className={activeTab === tab.id ? 'active' : ''}
                onClick={() => setActiveTab(tab.id)}
              >
                <i className={tab.icon}></i>
                <span className="tab-label">{tab.label}</span>
                <span className="tab-count">{tab.count}</span>
              </button>
            ))}
          </div>
          <div className="explorer-toolbar-actions">
            <div className="settings-menu-wrapper">
              <button
                className={`explorer-settings-btn ${showSettings ? 'active' : ''}`}
                onClick={() => setShowSettings(!showSettings)}
                title="Settings"
              >
                <i className="fa-solid fa-gear"></i>
              </button>
              <ExplorerSettingsMenu
                isOpen={showSettings}
                onClose={() => setShowSettings(false)}
                activeTab={activeTab}
                settings={settings}
                onSettingsChange={setSettings}
                onReset={handleResetSettings}
              />
            </div>
            <button
              className="prune-btn"
              onClick={() => setConfirmPrune(activeTab)}
              disabled={pruneLoading}
              title={`Prune unused ${activeTab}`}
            >
              {pruneLoading ? (
                <i className="fa-solid fa-circle-notch fa-spin"></i>
              ) : (
                <i className="fa-solid fa-broom"></i>
              )}
              <span>Prune</span>
            </button>
          </div>
        </div>
      </header>

      {/* Prune Confirmation Dialog */}
      {confirmPrune && (
        <div className="confirm-dialog-overlay" onClick={cancelPrune}>
          <div
            className="confirm-dialog"
            ref={pruneDialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="prune-dialog-title"
            onClick={e => e.stopPropagation()}
          >
            <div className="confirm-dialog-header">
              <h3 id="prune-dialog-title">Confirm Prune</h3>
            </div>
            <div className="confirm-dialog-body">
              <p>
                {confirmPrune === 'containers' && 'Remove all stopped containers?'}
                {confirmPrune === 'images' && 'Remove all dangling (unused) images?'}
                {confirmPrune === 'networks' && 'Remove all unused networks?'}
                {confirmPrune === 'volumes' && 'Remove all unused volumes?'}
              </p>
              <p className="confirm-warning">
                {confirmPrune === 'volumes' && (
                  <><i className="fa-solid fa-triangle-exclamation"></i> This will permanently delete volume data!</>
                )}
                {confirmPrune !== 'volumes' && 'This action cannot be undone.'}
              </p>
            </div>
            <div className="confirm-dialog-actions">
              <button className="confirm-cancel" onClick={cancelPrune}>Cancel</button>
              <button
                className="confirm-proceed confirm-danger"
                onClick={() => handlePrune(confirmPrune)}
                disabled={pruneLoading}
              >
                {pruneLoading ? 'Pruning...' : 'Prune'}
              </button>
            </div>
          </div>
        </div>
      )}

      <main className="explorer-content" ref={contentRef}>
        {activeTab === 'containers' && renderContainersTab()}
        {activeTab === 'images' && renderImagesTab()}
        {activeTab === 'networks' && renderNetworksTab()}
        {activeTab === 'volumes' && renderVolumesTab()}
      </main>
    </div>
  );
}
