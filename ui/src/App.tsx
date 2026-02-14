import { useState, useEffect, useCallback } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation, useNavigate } from 'react-router-dom'
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
import { STORAGE_KEY_TAB, ACTIVE_OPERATION_KEY } from './utils/constants'
import { getPageTitle } from './operation/utils'
// CSS is now imported via index.css

interface ActiveOperation {
  url: string;
  type: string | null;
  containerCount: number;
  startTime: number | null;
}

function ActiveOperationBanner({ op, onDismiss }: { op: ActiveOperation; onDismiss: () => void }) {
  const navigate = useNavigate();
  const label = getPageTitle(op.type as any) || 'Operation in progress';

  return (
    <div className="active-operation-banner">
      <div className="active-operation-banner-content" onClick={() => navigate(op.url)}>
        <i className="fa-solid fa-spinner fa-spin"></i>
        <span className="active-operation-banner-label">{label}</span>
        {op.containerCount > 1 && (
          <span className="active-operation-banner-count">({op.containerCount} containers)</span>
        )}
        <span className="active-operation-banner-view">View <i className="fa-solid fa-arrow-right"></i></span>
      </div>
      <button className="active-operation-banner-dismiss" onClick={(e) => { e.stopPropagation(); onDismiss(); }} aria-label="Dismiss">
        <i className="fa-solid fa-xmark"></i>
      </button>
    </div>
  );
}

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

  // Active operation banner state
  const [activeOp, setActiveOp] = useState<ActiveOperation | null>(() => {
    try {
      const saved = sessionStorage.getItem(ACTIVE_OPERATION_KEY);
      if (!saved) return null;
      const parsed = JSON.parse(saved);
      // Ignore if > 30 min old
      if (parsed.startTime && Date.now() - parsed.startTime > 30 * 60 * 1000) {
        sessionStorage.removeItem(ACTIVE_OPERATION_KEY);
        return null;
      }
      return parsed;
    } catch { return null; }
  });
  const [bannerDismissed, setBannerDismissed] = useState(false);

  // Determine if we're on a sub-page (hide tab bar)
  const isSubPage = location.pathname.startsWith('/container/') ||
                    location.pathname.startsWith('/tag-filter/') ||
                    location.pathname.startsWith('/operation') ||
                    location.pathname.startsWith('/explorer/container/');

  const isOnOperationPage = location.pathname.startsWith('/operation');

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

  // Poll for active operation banner — check sessionStorage periodically
  // (OperationProgressPage writes/clears it as phases change)
  useEffect(() => {
    const check = () => {
      try {
        const saved = sessionStorage.getItem(ACTIVE_OPERATION_KEY);
        if (!saved) {
          setActiveOp(null);
          setBannerDismissed(false);
          return;
        }
        const parsed = JSON.parse(saved);
        if (parsed.startTime && Date.now() - parsed.startTime > 30 * 60 * 1000) {
          sessionStorage.removeItem(ACTIVE_OPERATION_KEY);
          setActiveOp(null);
          return;
        }
        setActiveOp(parsed);
      } catch {
        setActiveOp(null);
      }
    };
    check();
    const interval = setInterval(check, 2000);
    return () => clearInterval(interval);
  }, []);

  // Also validate once on mount: if there's an active op, check if it's actually still running
  useEffect(() => {
    if (!activeOp) return;
    const groupMatch = activeOp.url.match(/group=([^&]+)/);
    const idMatch = activeOp.url.match(/id=([^&]+)/);
    const url = groupMatch
      ? `/api/operations/group/${groupMatch[1]}`
      : idMatch
      ? `/api/operations/${idMatch[1]}`
      : null;
    if (!url) return;

    fetch(url)
      .then(r => r.json())
      .then(data => {
        if (!data.success || !data.data) {
          sessionStorage.removeItem(ACTIVE_OPERATION_KEY);
          setActiveOp(null);
          return;
        }
        // For group endpoint, check if all operations are terminal
        const ops = data.data.operations || [data.data];
        const allDone = ops.every((op: any) => op.status === 'complete' || op.status === 'failed');
        if (allDone) {
          sessionStorage.removeItem(ACTIVE_OPERATION_KEY);
          setActiveOp(null);
        }
      })
      .catch(() => {});
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const showBanner = activeOp && !isOnOperationPage && !bannerDismissed;

  return (
    <div className="app">
      {showBanner && (
        <ActiveOperationBanner
          op={activeOp}
          onDismiss={() => setBannerDismissed(true)}
        />
      )}
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
