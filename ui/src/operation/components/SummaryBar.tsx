import { STAGE_INFO, RESTART_STAGES } from '../../constants/progress';
import type { OperationState } from '../types';
import { selectDisplayPercent, selectSuccessCount, selectFailedCount } from '../selectors';

interface SummaryBarProps {
  state: OperationState;
  isComplete: boolean;
  hasErrors: boolean;
  elapsedTime: number;
}

export function SummaryBar({ state, isComplete, hasErrors, elapsedTime }: SummaryBarProps) {
  const { containers, operationType } = state;
  const total = containers.length;
  const successCount = selectSuccessCount(state);
  const failedCount = selectFailedCount(state);
  const runningCount = containers.filter(c => c.status === 'in_progress').length;
  const queuedCount = containers.filter(c => c.status === 'pending').length;
  const completedCount = successCount + failedCount;
  const displayPercent = selectDisplayPercent(state);
  const activeContainer = containers.find(c => c.status === 'in_progress');

  const isRollback = operationType === 'rollback' || operationType === 'labelRollback';
  const barClass = isRollback ? 'warning' : 'accent';

  // "Now" line: active container stage, or completion message
  let nowText = '';
  if (isComplete) {
    if (hasErrors) {
      if (failedCount > 0 && successCount > 0) {
        nowText = `${successCount} succeeded, ${failedCount} failed`;
      } else if (failedCount > 0) {
        nowText = `${failedCount} failed`;
      } else {
        nowText = 'Operation failed';
      }
    } else {
      nowText = total > 1 ? `All ${total} completed` : 'Completed';
    }
  } else if (activeContainer) {
    const stageInfo = activeContainer.stage
      ? (STAGE_INFO[activeContainer.stage] || RESTART_STAGES[activeContainer.stage])
      : null;
    const stageLabel = stageInfo?.label || activeContainer.message || 'Processing';
    nowText = `${activeContainer.name} \u00b7 ${stageLabel}`;
  } else if (queuedCount > 0) {
    nowText = 'Waiting to start...';
  }

  const formatTime = (s: number) => {
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    const rem = s % 60;
    return `${m}m ${rem}s`;
  };

  return (
    <div className={`summary-bar ${isComplete ? (hasErrors ? 'complete-error' : 'complete-success') : ''}`}>
      {/* Progress bar */}
      <div className="summary-progress">
        <div className="summary-progress-track">
          <div
            className={`summary-progress-fill ${barClass}`}
            style={{ width: `${displayPercent}%` }}
          />
        </div>
        <div className="summary-progress-meta">
          <span className="summary-progress-count">{completedCount}/{total}</span>
          <span className="summary-progress-time">{formatTime(elapsedTime)}</span>
        </div>
      </div>

      {/* Status counters */}
      <div className="summary-counters">
        {successCount > 0 && (
          <span className="summary-counter done">
            <i className="fa-solid fa-check"></i> {successCount} Done
          </span>
        )}
        {runningCount > 0 && (
          <span className="summary-counter running">
            <i className="fa-solid fa-circle-notch fa-spin"></i> {runningCount} Running
          </span>
        )}
        {queuedCount > 0 && (
          <span className="summary-counter queued">
            <i className="fa-regular fa-clock"></i> {queuedCount} Queued
          </span>
        )}
        {failedCount > 0 && (
          <span className="summary-counter failed">
            <i className="fa-solid fa-xmark"></i> {failedCount} Failed
          </span>
        )}
      </div>

      {/* "Now" line */}
      {nowText && (
        <div className={`summary-now ${isComplete ? (hasErrors ? 'now-error' : 'now-success') : ''}`}>
          {!isComplete && <span className="now-label">Now:</span>}
          <span className="now-text">{nowText}</span>
        </div>
      )}
    </div>
  );
}
