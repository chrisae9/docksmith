import { useState, useEffect, useCallback } from 'react'
import { Dashboard } from './components/Dashboard'
import { History } from './components/History'
import { Settings } from './components/Settings'
import { TabBar, type TabId } from './components/TabBar'
import { getContainerStatus } from './api/client'
import { useEventStream } from './hooks/useEventStream'
import { STORAGE_KEY_TAB } from './utils/constants'
import './App.css'

function App() {
  const [activeTab, setActiveTab] = useState<TabId>(() => {
    // Restore last active tab from localStorage
    const saved = localStorage.getItem(STORAGE_KEY_TAB);
    return (saved as TabId) || 'updates';
  });
  const [updateCount, setUpdateCount] = useState(0);
  const { lastEvent, containerUpdated } = useEventStream(true);

  // Save active tab to localStorage whenever it changes
  useEffect(() => {
    localStorage.setItem(STORAGE_KEY_TAB, activeTab);
  }, [activeTab]);

  const fetchUpdateCount = useCallback(async () => {
    try {
      const result = await getContainerStatus();
      if (result.success && result.data) {
        const pinnableCount = result.data.containers.filter(
          c => c.status === 'UP_TO_DATE_PINNABLE'
        ).length;
        setUpdateCount(result.data.updates_found + pinnableCount);
      }
    } catch {
      // Silently fail - badge will show 0
    }
  }, []);

  // Fetch update count for badge
  useEffect(() => {
    fetchUpdateCount();
    // Refresh count every 5 minutes
    const interval = setInterval(fetchUpdateCount, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, [fetchUpdateCount]);

  // Refresh badge count when update completes
  useEffect(() => {
    if (lastEvent && (lastEvent.stage === 'complete' || lastEvent.stage === 'failed')) {
      // Delay slightly to let API update
      setTimeout(fetchUpdateCount, 1000);
    }
  }, [lastEvent, fetchUpdateCount]);

  // Refresh badge count when container status changes (from background check, rollback, etc.)
  useEffect(() => {
    if (containerUpdated) {
      // Delay slightly to ensure cache is updated
      setTimeout(fetchUpdateCount, 500);
    }
  }, [containerUpdated, fetchUpdateCount]);

  const renderTabContent = () => {
    switch (activeTab) {
      case 'updates':
        return <Dashboard onNavigateToHistory={() => setActiveTab('history')} />;
      case 'history':
        return <History onBack={() => setActiveTab('updates')} />;
      case 'settings':
        return <Settings onBack={() => setActiveTab('updates')} />;
      default:
        return null;
    }
  };

  return (
    <div className="app">
      <div className="tab-content">
        {renderTabContent()}
      </div>
      <TabBar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        updateCount={updateCount}
      />
    </div>
  )
}

export default App
