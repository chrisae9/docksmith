
export type ContainerSortBy = 'name' | 'status' | 'created' | 'image';
export type ContainerGroupBy = 'stack' | 'none' | 'status' | 'image' | 'network';
export type ImageSortBy = 'tags' | 'size' | 'created' | 'id';
export type ImageGroupBy = 'none' | 'repository';
export type NetworkSortBy = 'name' | 'driver' | 'containers' | 'created';
export type NetworkGroupBy = 'none' | 'driver';
export type VolumeSortBy = 'name' | 'size' | 'created' | 'driver';
export type VolumeGroupBy = 'none' | 'type';

export interface ExplorerSettings {
  containers: {
    sortBy: ContainerSortBy;
    groupBy: ContainerGroupBy;
    ascending: boolean;
    showLabels: boolean;
    showRunningOnly: boolean;
  };
  images: {
    sortBy: ImageSortBy;
    groupBy: ImageGroupBy;
    ascending: boolean;
    showDanglingOnly: boolean;
  };
  networks: {
    sortBy: NetworkSortBy;
    groupBy: NetworkGroupBy;
    ascending: boolean;
    hideBuiltIn: boolean;
  };
  volumes: {
    sortBy: VolumeSortBy;
    groupBy: VolumeGroupBy;
    ascending: boolean;
    showUnusedOnly: boolean;
  };
}

export const DEFAULT_SETTINGS: ExplorerSettings = {
  containers: {
    sortBy: 'name',
    groupBy: 'stack',
    ascending: true,
    showLabels: false,
    showRunningOnly: false,
  },
  images: {
    sortBy: 'tags',
    groupBy: 'repository',
    ascending: true,
    showDanglingOnly: false,
  },
  networks: {
    sortBy: 'name',
    groupBy: 'none',
    ascending: true,
    hideBuiltIn: false,
  },
  volumes: {
    sortBy: 'name',
    groupBy: 'type',
    ascending: true,
    showUnusedOnly: false,
  },
};

interface ExplorerSettingsMenuProps {
  isOpen: boolean;
  onClose: () => void;
  activeTab: 'containers' | 'images' | 'networks' | 'volumes';
  settings: ExplorerSettings;
  onSettingsChange: (settings: ExplorerSettings) => void;
  onReset: () => void;
}

import { useEffect } from 'react';

export function ExplorerSettingsMenu({
  isOpen,
  onClose,
  activeTab,
  settings,
  onSettingsChange,
  onReset,
}: ExplorerSettingsMenuProps) {
  // Close on Escape key
  useEffect(() => {
    if (!isOpen) return;
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const updateContainerSettings = (updates: Partial<ExplorerSettings['containers']>) => {
    onSettingsChange({
      ...settings,
      containers: { ...settings.containers, ...updates },
    });
  };

  const updateImageSettings = (updates: Partial<ExplorerSettings['images']>) => {
    onSettingsChange({
      ...settings,
      images: { ...settings.images, ...updates },
    });
  };

  const updateNetworkSettings = (updates: Partial<ExplorerSettings['networks']>) => {
    onSettingsChange({
      ...settings,
      networks: { ...settings.networks, ...updates },
    });
  };

  const updateVolumeSettings = (updates: Partial<ExplorerSettings['volumes']>) => {
    onSettingsChange({
      ...settings,
      volumes: { ...settings.volumes, ...updates },
    });
  };

  return (
    <>
      {/* Invisible overlay to capture clicks outside and prevent propagation */}
      <div
        className="settings-menu-backdrop"
        onClick={(e) => {
          e.stopPropagation();
          onClose();
        }}
      />
      <div className="settings-menu" role="dialog" aria-label="View options">
        <div className="settings-menu-header">
        <span>View Options</span>
        <button className="settings-reset" onClick={onReset} title="Reset to defaults" aria-label="Reset to defaults">
          <i className="fa-solid fa-rotate-left" aria-hidden="true"></i>
        </button>
      </div>

      {activeTab === 'containers' && (
        <div className="settings-menu-content">
          <div className="settings-group">
            <label className="settings-label">Group by</label>
            <div className="settings-options">
              {(['stack', 'status', 'image', 'network', 'none'] as const).map(option => (
                <button
                  key={option}
                  className={settings.containers.groupBy === option ? 'active' : ''}
                  onClick={() => updateContainerSettings({ groupBy: option })}
                >
                  {option === 'stack' && 'Stack'}
                  {option === 'status' && 'Status'}
                  {option === 'image' && 'Image'}
                  {option === 'network' && 'Network'}
                  {option === 'none' && 'None'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Sort by</label>
            <div className="settings-options">
              {(['name', 'status', 'created', 'image'] as const).map(option => (
                <button
                  key={option}
                  className={settings.containers.sortBy === option ? 'active' : ''}
                  onClick={() => updateContainerSettings({ sortBy: option })}
                >
                  {option === 'name' && 'Name'}
                  {option === 'status' && 'Status'}
                  {option === 'created' && 'Created'}
                  {option === 'image' && 'Image'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Order</label>
            <div className="settings-options">
              <button
                className={settings.containers.ascending ? 'active' : ''}
                onClick={() => updateContainerSettings({ ascending: true })}
              >
                <i className="fa-solid fa-arrow-up-a-z"></i> Asc
              </button>
              <button
                className={!settings.containers.ascending ? 'active' : ''}
                onClick={() => updateContainerSettings({ ascending: false })}
              >
                <i className="fa-solid fa-arrow-down-z-a"></i> Desc
              </button>
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.containers.showRunningOnly}
                onChange={e => updateContainerSettings({ showRunningOnly: e.target.checked })}
              />
              <span>Running only</span>
            </label>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.containers.showLabels}
                onChange={e => updateContainerSettings({ showLabels: e.target.checked })}
              />
              <span>Show labels</span>
            </label>
          </div>
        </div>
      )}

      {activeTab === 'images' && (
        <div className="settings-menu-content">
          <div className="settings-group">
            <label className="settings-label">Group by</label>
            <div className="settings-options">
              {(['none', 'repository'] as const).map(option => (
                <button
                  key={option}
                  className={settings.images.groupBy === option ? 'active' : ''}
                  onClick={() => updateImageSettings({ groupBy: option })}
                >
                  {option === 'none' && 'None'}
                  {option === 'repository' && 'Repository'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Sort by</label>
            <div className="settings-options">
              {(['tags', 'size', 'created', 'id'] as const).map(option => (
                <button
                  key={option}
                  className={settings.images.sortBy === option ? 'active' : ''}
                  onClick={() => updateImageSettings({ sortBy: option })}
                >
                  {option === 'tags' && 'Tags'}
                  {option === 'size' && 'Size'}
                  {option === 'created' && 'Created'}
                  {option === 'id' && 'ID'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Order</label>
            <div className="settings-options">
              <button
                className={settings.images.ascending ? 'active' : ''}
                onClick={() => updateImageSettings({ ascending: true })}
              >
                <i className="fa-solid fa-arrow-up-a-z"></i> Asc
              </button>
              <button
                className={!settings.images.ascending ? 'active' : ''}
                onClick={() => updateImageSettings({ ascending: false })}
              >
                <i className="fa-solid fa-arrow-down-z-a"></i> Desc
              </button>
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.images.showDanglingOnly}
                onChange={e => updateImageSettings({ showDanglingOnly: e.target.checked })}
              />
              <span>Show dangling only</span>
            </label>
          </div>
        </div>
      )}

      {activeTab === 'networks' && (
        <div className="settings-menu-content">
          <div className="settings-group">
            <label className="settings-label">Group by</label>
            <div className="settings-options">
              {(['none', 'driver'] as const).map(option => (
                <button
                  key={option}
                  className={settings.networks.groupBy === option ? 'active' : ''}
                  onClick={() => updateNetworkSettings({ groupBy: option })}
                >
                  {option === 'none' && 'None'}
                  {option === 'driver' && 'Driver'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Sort by</label>
            <div className="settings-options">
              {(['name', 'driver', 'containers', 'created'] as const).map(option => (
                <button
                  key={option}
                  className={settings.networks.sortBy === option ? 'active' : ''}
                  onClick={() => updateNetworkSettings({ sortBy: option })}
                >
                  {option === 'name' && 'Name'}
                  {option === 'driver' && 'Driver'}
                  {option === 'containers' && 'Containers'}
                  {option === 'created' && 'Created'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Order</label>
            <div className="settings-options">
              <button
                className={settings.networks.ascending ? 'active' : ''}
                onClick={() => updateNetworkSettings({ ascending: true })}
              >
                <i className="fa-solid fa-arrow-up-a-z"></i> Asc
              </button>
              <button
                className={!settings.networks.ascending ? 'active' : ''}
                onClick={() => updateNetworkSettings({ ascending: false })}
              >
                <i className="fa-solid fa-arrow-down-z-a"></i> Desc
              </button>
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.networks.hideBuiltIn}
                onChange={e => updateNetworkSettings({ hideBuiltIn: e.target.checked })}
              />
              <span>Hide built-in</span>
            </label>
          </div>
        </div>
      )}

      {activeTab === 'volumes' && (
        <div className="settings-menu-content">
          <div className="settings-group">
            <label className="settings-label">Group by</label>
            <div className="settings-options">
              {(['none', 'type'] as const).map(option => (
                <button
                  key={option}
                  className={settings.volumes.groupBy === option ? 'active' : ''}
                  onClick={() => updateVolumeSettings({ groupBy: option })}
                >
                  {option === 'none' && 'None'}
                  {option === 'type' && 'Type'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Sort by</label>
            <div className="settings-options">
              {(['name', 'size', 'created', 'driver'] as const).map(option => (
                <button
                  key={option}
                  className={settings.volumes.sortBy === option ? 'active' : ''}
                  onClick={() => updateVolumeSettings({ sortBy: option })}
                >
                  {option === 'name' && 'Name'}
                  {option === 'size' && 'Size'}
                  {option === 'created' && 'Created'}
                  {option === 'driver' && 'Driver'}
                </button>
              ))}
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-label">Order</label>
            <div className="settings-options">
              <button
                className={settings.volumes.ascending ? 'active' : ''}
                onClick={() => updateVolumeSettings({ ascending: true })}
              >
                <i className="fa-solid fa-arrow-up-a-z"></i> Asc
              </button>
              <button
                className={!settings.volumes.ascending ? 'active' : ''}
                onClick={() => updateVolumeSettings({ ascending: false })}
              >
                <i className="fa-solid fa-arrow-down-z-a"></i> Desc
              </button>
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.volumes.showUnusedOnly}
                onChange={e => updateVolumeSettings({ showUnusedOnly: e.target.checked })}
              />
              <span>Show unused only</span>
            </label>
          </div>
        </div>
      )}
    </div>
    </>
  );
}
