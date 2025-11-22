import React from 'react';
import type { UpdateProgressEvent } from '../hooks/useEventStream';

export interface ProgressModalStatCard {
  label: string;
  value: string | number;
  variant?: 'success' | 'error' | 'default';
}

export interface ProgressModalContainer {
  name: string;
  status: 'pending' | 'in_progress' | 'success' | 'failed';
  message?: string;
  error?: string;
}

export interface ProgressModalLog {
  time: number;
  message: string;
}

export interface ProgressModalProps {
  title: string;

  // Stage display
  stageIcon: React.ReactNode;
  stageVariant: 'in-progress' | 'complete' | 'complete-with-errors';
  stageMessage: string;

  // Stats cards
  stats: ProgressModalStatCard[];

  // Container list (optional - for multi-container operations)
  containers?: ProgressModalContainer[];

  // Current operation progress (optional)
  currentProgress?: {
    event: UpdateProgressEvent;
    getStageIcon: (stage: string) => React.ReactNode;
  };

  // Activity logs
  logs: ProgressModalLog[];
  logEntriesRef: React.RefObject<HTMLDivElement | null>;

  // Footer button
  buttonText: string;
  buttonDisabled: boolean;
  onClose: () => void;
}

export function ProgressModal({
  title,
  stageIcon,
  stageVariant,
  stageMessage,
  stats,
  containers,
  currentProgress,
  logs,
  logEntriesRef,
  buttonText,
  buttonDisabled,
  onClose,
}: ProgressModalProps) {
  return (
    <div className="update-progress-overlay">
      <div className="update-progress-modal tui-style">
        {/* Header */}
        <div className="update-progress-header">
          <h3>{title}</h3>
        </div>

        {/* Stage Display */}
        <div className="update-stage-display">
          <div className={`update-stage-icon ${stageVariant}`}>
            {stageIcon}
          </div>
          <div className="update-stage-message">
            {stageMessage}
          </div>
        </div>

        {/* Stats Cards */}
        <div className="update-stats-cards" style={{ gridTemplateColumns: `repeat(${stats.length}, 1fr)` }}>
          {stats.map((stat, index) => (
            <div key={index} className={`stat-card ${stat.variant || ''}`}>
              <div className="stat-label">{stat.label}</div>
              <div className="stat-value">{stat.value}</div>
            </div>
          ))}
        </div>

        {/* Container List (optional) */}
        {containers && containers.length > 0 && (
          <div className="update-container-list">
            {containers.map((container, index) => (
              <div key={container.name} className={`update-container-item status-${container.status}`}>
                <span className="status-icon">
                  {container.status === 'pending' && <i className="fa-regular fa-circle"></i>}
                  {container.status === 'in_progress' && <i className="fa-solid fa-spinner fa-spin"></i>}
                  {container.status === 'success' && <i className="fa-solid fa-check"></i>}
                  {container.status === 'failed' && <i className="fa-solid fa-xmark"></i>}
                </span>
                <span className="container-index">{index + 1}.</span>
                <span className="container-name">{container.name}</span>
                {container.message && (
                  <span className="container-message">- {container.message}</span>
                )}
                {container.error && (
                  <div className="container-error">Error: {container.error}</div>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Current Operation Progress (optional) */}
        {currentProgress && (
          <div className="current-operation-progress">
            <div className="update-progress-bar">
              <div
                className="update-progress-bar-fill"
                style={{ width: `${currentProgress.event.progress ?? currentProgress.event.percent ?? 0}%` }}
              />
              <span className="update-progress-bar-text">
                {currentProgress.event.progress ?? currentProgress.event.percent ?? 0}%
              </span>
            </div>
            <div className="update-progress-stage">
              {currentProgress.getStageIcon(currentProgress.event.stage)} {currentProgress.event.message}
            </div>
          </div>
        )}

        {/* Activity Log */}
        <div className="update-activity-log">
          <div className="log-header">Recent Activity:</div>
          <div className="log-entries" ref={logEntriesRef}>
            {logs.slice(-10).map((log, i) => (
              <div key={i} className="log-entry">
                <span className="log-time">
                  [{new Date(log.time).toLocaleTimeString('en-US', { hour12: false })}]
                </span>
                <span className="log-message">{log.message}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Footer */}
        <div className="update-footer">
          <button
            className="btn-primary update-close-btn"
            onClick={onClose}
            disabled={buttonDisabled}
            style={{ width: '100%', padding: '10px 16px', fontSize: '14px' }}
          >
            {buttonText}
          </button>
        </div>
      </div>
    </div>
  );
}
