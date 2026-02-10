import { useState, useEffect, useCallback } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import { Containers } from './components/Containers'
import { History } from './components/History'
import { Settings } from './components/Settings'
import { TagFilterPage } from './pages/TagFilterPage'
import { ContainerPage } from './pages/ContainerPage'
import { ScriptSelectionPage } from './pages/ScriptSelectionPage'
import { RestartDependenciesPage } from './pages/RestartDependenciesPage'
import { OperationProgressPage } from './pages/OperationProgressPage'
import { TabBar, type TabId } from './components/TabBar'
import { ToastProvider, ToastContainer } from './components/Toast'
import { getContainerStatus } from './api/client'
import { useEventStream } from './hooks/useEventStream'
import { STORAGE_KEY_TAB } from './utils/constants'
// CSS is now imported via index.css

function AppContent() {
  const location = useLocation();

  const [activeTab, setActiveTab] = useState<TabId>(() => {
    const saved = localStorage.getItem(STORAGE_KEY_TAB);
    // Migrate old tab IDs
    if (saved === 'updates' || saved === 'explorer') return 'containers';
    return (saved as TabId) || 'containers';
  });
  const [updateCount, setUpdateCount] = useState(0);
  const { lastEvent, containerUpdated } = useEventStream(true);

  // Determine if we're on a sub-page (hide tab bar)
  const isSubPage = location.pathname.startsWith('/container/') ||
                    location.pathname.startsWith('/tag-filter/') ||
                    location.pathname.startsWith('/operation') ||
                    location.pathname.startsWith('/explorer/container/');

  // Sync activeTab state with URL for tab highlighting
  useEffect(() => {
    const path = location.pathname;
    if (path === '/' || path === '/containers') {
      setActiveTab('containers');
    } else if (path === '/history') {
      setActiveTab('history');
    } else if (path === '/settings') {
      setActiveTab('settings');
    }
  }, [location.pathname]);

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
    const interval = setInterval(fetchUpdateCount, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, [fetchUpdateCount]);

  // Refresh badge count when update completes
  useEffect(() => {
    if (lastEvent && (lastEvent.stage === 'complete' || lastEvent.stage === 'failed')) {
      setTimeout(fetchUpdateCount, 1000);
    }
  }, [lastEvent, fetchUpdateCount]);

  // Refresh badge count when container status changes
  useEffect(() => {
    if (containerUpdated) {
      setTimeout(fetchUpdateCount, 500);
    }
  }, [containerUpdated, fetchUpdateCount]);

  return (
    <div className="app">
      <div className="tab-content">
        <Routes>
          <Route path="/" element={<Containers />} />
          <Route path="/containers" element={<Containers />} />
          {/* Redirects from old routes */}
          <Route path="/updates" element={<Navigate to="/" replace />} />
          <Route path="/explorer" element={<Navigate to="/" replace />} />
          <Route path="/history" element={<History onBack={() => setActiveTab('containers')} />} />
          <Route path="/settings" element={<Settings onBack={() => setActiveTab('containers')} />} />
          <Route path="/container/:containerName" element={<ContainerPage />} />
          <Route path="/container/:containerName/tag-filter" element={<TagFilterPage />} />
          <Route path="/container/:containerName/script-selection" element={<ScriptSelectionPage />} />
          <Route path="/container/:containerName/restart-dependencies" element={<RestartDependenciesPage />} />
          <Route path="/operation" element={<OperationProgressPage />} />
          <Route path="/explorer/container/:name" element={<ContainerPage />} />
        </Routes>
      </div>
      {!isSubPage && (
        <TabBar
          activeTab={activeTab}
          onTabChange={setActiveTab}
          updateCount={updateCount}
        />
      )}
    </div>
  )
}

function App() {
  return (
    <ToastProvider>
      <BrowserRouter>
        <AppContent />
        <ToastContainer />
      </BrowserRouter>
    </ToastProvider>
  );
}

export default App
