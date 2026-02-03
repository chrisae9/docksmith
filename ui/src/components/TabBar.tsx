import React from 'react';
import { useNavigate } from 'react-router-dom';

export type TabId = 'updates' | 'explorer' | 'history' | 'settings';

interface Tab {
  id: TabId;
  label: string;
  icon: string;
  activeIcon: string;
  badge?: number;
  path: string;
}

interface TabBarProps {
  activeTab: TabId;
  onTabChange: (tab: TabId) => void;
  updateCount?: number;
}

export const TabBar: React.FC<TabBarProps> = ({ activeTab, onTabChange, updateCount = 0 }) => {
  const navigate = useNavigate();

  const tabs: Tab[] = [
    {
      id: 'updates',
      label: 'Updates',
      icon: 'fa-solid fa-arrow-down',
      activeIcon: 'fa-solid fa-arrow-down',
      badge: updateCount > 0 ? updateCount : undefined,
      path: '/',
    },
    {
      id: 'explorer',
      label: 'Explorer',
      icon: 'fa-solid fa-layer-group',
      activeIcon: 'fa-solid fa-layer-group',
      path: '/explorer',
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

  return (
    <nav className="tab-bar" role="tablist" aria-label="Main navigation">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={activeTab === tab.id}
          aria-label={tab.badge ? `${tab.label}, ${tab.badge} items` : tab.label}
          className={`tab-item ${activeTab === tab.id ? 'active' : ''}`}
          onClick={() => handleTabClick(tab)}
        >
          <div className="tab-icon-container">
            <i className={`tab-icon ${activeTab === tab.id ? tab.activeIcon : tab.icon}`} aria-hidden="true"></i>
            {tab.badge !== undefined && (
              <span className="tab-badge" aria-hidden="true">{tab.badge > 99 ? '99+' : tab.badge}</span>
            )}
          </div>
          <span className="tab-label">{tab.label}</span>
        </button>
      ))}
    </nav>
  );
};
