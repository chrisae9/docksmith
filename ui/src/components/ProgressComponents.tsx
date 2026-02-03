import { STAGE_INFO, RESTART_STAGES, type LogEntry } from '../constants/progress';

interface ContainerProgress {
  name: string;
  status: 'pending' | 'in_progress' | 'success' | 'failed';
  stage?: string;
  percent?: number;
  message?: string;
  error?: string;
  operationId?: string;
  badge?: string;
  versionFrom?: string;
  versionTo?: string;
}

interface ContainerProgressRowProps {
  container: ContainerProgress;
}

export function ContainerProgressRow({ container }: ContainerProgressRowProps) {
  return (
    <div className={`container-item status-${container.status}`}>
      <div className="container-main-row">
        <span className="status-icon">
          {container.status === 'pending' && <i className="fa-regular fa-circle"></i>}
          {container.status === 'in_progress' && (
            container.stage && (STAGE_INFO[container.stage] || RESTART_STAGES[container.stage])
              ? <i className={`fa-solid ${(STAGE_INFO[container.stage] || RESTART_STAGES[container.stage]).icon}`}></i>
              : <i className="fa-solid fa-spinner fa-spin"></i>
          )}
          {container.status === 'success' && <i className="fa-solid fa-circle-check"></i>}
          {container.status === 'failed' && <i className="fa-solid fa-circle-xmark"></i>}
        </span>
        <span className="container-name">{container.name}</span>
        {container.badge && <span className={`container-badge ${container.badge.toLowerCase()}`}>{container.badge}</span>}
        {container.status === 'in_progress' && container.percent !== undefined && container.percent > 0 && (
          <span className="container-percent">{container.percent}%</span>
        )}
      </div>
      {container.message && (
        <div className="container-message">{container.message}</div>
      )}
      {container.versionFrom && container.versionTo && (
        <div className="container-version">
          <span className="version-current">{container.versionFrom}</span>
          <span className="version-arrow">→</span>
          <span className="version-target">{container.versionTo}</span>
        </div>
      )}
      {container.status === 'in_progress' && container.percent !== undefined && container.percent > 0 && (
        <div className="container-progress-bar">
          <div className="container-progress-fill" style={{ width: `${container.percent}%` }} />
        </div>
      )}
    </div>
  );
}

interface ProgressStatsProps {
  total: number;
  successCount: number;
  failedCount: number;
  elapsedTime: number;
  isComplete: boolean;
}

export function ProgressStats({ total, successCount, failedCount, elapsedTime, isComplete }: ProgressStatsProps) {
  return (
    <div className="progress-stats">
      <div className="stat-card">
        <span className="stat-label">Progress</span>
        <span className="stat-value">{isComplete ? total : successCount + failedCount}/{total}</span>
      </div>
      <div className={`stat-card ${successCount > 0 ? 'success' : ''}`}>
        <span className="stat-label">Successful</span>
        <span className="stat-value">{successCount}</span>
      </div>
      <div className={`stat-card ${failedCount > 0 ? 'error' : ''}`}>
        <span className="stat-label">Failed</span>
        <span className="stat-value">{failedCount}</span>
      </div>
      <div className="stat-card">
        <span className="stat-label">Elapsed</span>
        <span className="stat-value">{elapsedTime}s</span>
      </div>
    </div>
  );
}

interface ActivityLogProps {
  logs: LogEntry[];
  logRef?: React.RefObject<HTMLDivElement | null>;
}

export function ActivityLog({ logs, logRef }: ActivityLogProps) {
  return (
    <section className="activity-section">
      <h2><i className="fa-solid fa-list-check"></i> Activity Log</h2>
      <div className="activity-log" ref={logRef}>
        {logs.map((log, i) => (
          <div key={i} className={`log-entry log-${log.type}`}>
            <span className="log-time">
              [{new Date(log.time).toLocaleTimeString('en-US', { hour12: false })}]
            </span>
            {log.icon && (
              <span className="log-icon">
                <i className={`fa-solid ${log.icon}`}></i>
              </span>
            )}
            <span className="log-message">{log.message}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

// Re-export the ContainerProgress type for use elsewhere
export type { ContainerProgress };
