import React from 'react';
import { useNavigate } from 'react-router-dom';

export type TabId = 'containers' | 'history' | 'settings';

interface Tab {
  id: TabId;
  label: string;
  icon: string;
  activeIcon: string;
  path: string;
}

interface TabBarProps {
  activeTab: TabId;
  onTabChange: (tab: TabId) => void;
  updateCount?: number;
  pinnableCount?: number;
}

export const TabBar: React.FC<TabBarProps> = ({
  activeTab,
  onTabChange,
  updateCount = 0,
  pinnableCount = 0,
}) => {
  const navigate = useNavigate();

  const badgeUpdate = updateCount > 0 ? updateCount : undefined;
  const badgePinnable = pinnableCount > 0 ? pinnableCount : undefined;

  const tabs: Tab[] = [
    {
      id: 'containers',
      label: 'Containers',
      icon: 'fa-solid fa-cubes',
      activeIcon: 'fa-solid fa-cubes',
      path: '/',
    },
    {
      id: 'history',
      label: 'History',
      icon: 'fa-solid fa-clock-rotate-left',
      activeIcon: 'fa-solid fa-clock-rotate-left',
      path: '/history',
    },
    {
      id: 'settings',
      label: 'Settings',
      icon: 'fa-solid fa-gear',
      activeIcon: 'fa-solid fa-gear',
      path: '/settings',
    },
  ];

  const handleTabClick = (tab: Tab) => {
    onTabChange(tab.id);
    navigate(tab.path);
  };

  const getAriaLabel = (tab: Tab) => {
    if (tab.id !== 'containers') return tab.label;
    return `${tab.label}, ${updateCount} update${updateCount === 1 ? '' : 's'} available, ${pinnableCount} container${pinnableCount === 1 ? '' : 's'} on a floating tag`;
  };

  return (
    <nav className="tab-bar" role="tablist" aria-label="Main navigation">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={activeTab === tab.id}
          aria-label={getAriaLabel(tab)}
          className={`tab-item ${activeTab === tab.id ? 'active' : ''}`}
          onClick={() => handleTabClick(tab)}
        >
          <div className="tab-icon-container">
            <i className={`tab-icon ${activeTab === tab.id ? tab.activeIcon : tab.icon}`} aria-hidden="true"></i>
            {tab.id === 'containers' && (badgeUpdate !== undefined || badgePinnable !== undefined) && (
              <span className="tab-badges">
                {badgeUpdate !== undefined && (
                  <span
                    className="tab-badge update"
                    title={`${badgeUpdate} update${badgeUpdate === 1 ? '' : 's'} available`}
                    aria-hidden="true"
                  >
                    {badgeUpdate > 99 ? '99+' : badgeUpdate}
                  </span>
                )}
                {badgePinnable !== undefined && (
                  <span
                    className="tab-badge pinnable"
                    title={`${badgePinnable} container${badgePinnable === 1 ? '' : 's'} on a floating tag`}
                    aria-hidden="true"
                  >
                    {badgePinnable > 99 ? '99+' : badgePinnable}
                  </span>
                )}
              </span>
            )}
          </div>
          <span className="tab-label">{tab.label}</span>
        </button>
      ))}
    </nav>
  );
};
