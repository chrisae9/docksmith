import React from 'react';

export type TabId = 'updates' | 'history' | 'settings';

interface Tab {
  id: TabId;
  label: string;
  icon: string;
  activeIcon: string;
  badge?: number;
}

interface TabBarProps {
  activeTab: TabId;
  onTabChange: (tab: TabId) => void;
  updateCount?: number;
}

export const TabBar: React.FC<TabBarProps> = ({ activeTab, onTabChange, updateCount = 0 }) => {
  const tabs: Tab[] = [
    {
      id: 'updates',
      label: 'Updates',
      icon: 'fa-solid fa-arrow-down',
      activeIcon: 'fa-solid fa-arrow-down',
      badge: updateCount > 0 ? updateCount : undefined,
    },
    {
      id: 'history',
      label: 'History',
      icon: 'fa-solid fa-clock-rotate-left',
      activeIcon: 'fa-solid fa-clock-rotate-left',
    },
    {
      id: 'settings',
      label: 'Settings',
      icon: 'fa-solid fa-gear',
      activeIcon: 'fa-solid fa-gear',
    },
  ];

  return (
    <nav className="tab-bar">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          className={`tab-item ${activeTab === tab.id ? 'active' : ''}`}
          onClick={() => onTabChange(tab.id)}
        >
          <div className="tab-icon-container">
            <i className={`tab-icon ${activeTab === tab.id ? tab.activeIcon : tab.icon}`}></i>
            {tab.badge !== undefined && (
              <span className="tab-badge">{tab.badge > 99 ? '99+' : tab.badge}</span>
            )}
          </div>
          <span className="tab-label">{tab.label}</span>
        </button>
      ))}
    </nav>
  );
};
