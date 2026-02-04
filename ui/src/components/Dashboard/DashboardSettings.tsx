export type FilterType = 'all' | 'updates';
export type SortType = 'stack' | 'name';

export interface DashboardSettings {
  filter: FilterType;
  sort: SortType;
  showIgnored: boolean;
  showLocalImages: boolean;
  showMismatch: boolean;
}

export const DEFAULT_DASHBOARD_SETTINGS: DashboardSettings = {
  filter: 'updates',
  sort: 'stack',
  showIgnored: false,
  showLocalImages: false,
  showMismatch: true,
};

interface DashboardSettingsMenuProps {
  settings: DashboardSettings;
  onSettingsChange: (settings: Partial<DashboardSettings>) => void;
  onClose: () => void;
}

import { useEffect } from 'react';

export function DashboardSettingsMenu({
  settings,
  onSettingsChange,
  onClose,
}: DashboardSettingsMenuProps) {
  // Close on Escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [onClose]);

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
          <button className="settings-close-btn" onClick={onClose} aria-label="Close settings">
            <i className="fa-solid fa-xmark"></i>
          </button>
        </div>

        <div className="settings-menu-content">
          <div className="settings-group">
            <label className="settings-label">Group by</label>
            <div className="settings-options">
              <button
                className={settings.sort === 'stack' ? 'active' : ''}
                onClick={() => onSettingsChange({ sort: 'stack' })}
              >
                Stack
              </button>
              <button
                className={settings.sort === 'name' ? 'active' : ''}
                onClick={() => onSettingsChange({ sort: 'name' })}
              >
                None
              </button>
            </div>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.showIgnored}
                onChange={(e) => onSettingsChange({ showIgnored: e.target.checked })}
              />
              <span>Show ignored containers</span>
            </label>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.showLocalImages}
                onChange={(e) => onSettingsChange({ showLocalImages: e.target.checked })}
              />
              <span>Show local images</span>
            </label>
          </div>

          <div className="settings-group">
            <label className="settings-checkbox">
              <input
                type="checkbox"
                checked={settings.showMismatch}
                onChange={(e) => onSettingsChange({ showMismatch: e.target.checked })}
              />
              <span>Show mismatched containers</span>
            </label>
          </div>
        </div>
      </div>
    </>
  );
}
