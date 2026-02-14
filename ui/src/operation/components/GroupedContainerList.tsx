import { useState, useEffect } from 'react';
import { STAGE_INFO, RESTART_STAGES } from '../../constants/progress';
import type { ContainerState } from '../types';

interface GroupedContainerListProps {
  containers: ContainerState[];
  expectedDependents: string[];
  dependentsRestarted: string[];
  dependentsBlocked: string[];
  currentStage: string | null;
}

function ContainerRow({ container }: { container: ContainerState }) {
  const stageInfo = container.stage
    ? (STAGE_INFO[container.stage] || RESTART_STAGES[container.stage])
    : null;

  switch (container.status) {
    case 'pending':
      return (
        <div className="cr cr-pending">
          <span className="cr-icon"><i className="fa-regular fa-clock"></i></span>
          <span className="cr-name">{container.name}</span>
          {container.badge && <span className={`container-badge ${container.badge.toLowerCase()}`}>{container.badge}</span>}
          {container.versionFrom && container.versionTo && (
            <div className="cr-version">
              <span className="version-current">{container.versionFrom}</span>
              <span className="version-arrow">&rarr;</span>
              <span className="version-target">{container.versionTo}</span>
            </div>
          )}
        </div>
      );

    case 'in_progress':
      return (
        <div className="cr cr-running">
          <span className="cr-icon">
            {stageInfo
              ? <i className={`fa-solid ${stageInfo.icon}`}></i>
              : <i className="fa-solid fa-spinner fa-spin"></i>
            }
          </span>
          <span className="cr-name">{container.name}</span>
          <span className="cr-stage">{stageInfo?.label || container.message || 'Processing'}</span>
          {container.badge && <span className={`container-badge ${container.badge.toLowerCase()}`}>{container.badge}</span>}
          {container.versionFrom && container.versionTo && (
            <div className="cr-version">
              <span className="version-current">{container.versionFrom}</span>
              <span className="version-arrow">&rarr;</span>
              <span className="version-target">{container.versionTo}</span>
            </div>
          )}
        </div>
      );

    case 'success':
      return (
        <div className="cr cr-success">
          <span className="cr-icon"><i className="fa-solid fa-circle-check"></i></span>
          <span className="cr-name">{container.name}</span>
          <span className="cr-result">{container.message || 'Completed'}</span>
          {container.badge && <span className={`container-badge ${container.badge.toLowerCase()}`}>{container.badge}</span>}
          {container.versionFrom && container.versionTo && (
            <div className="cr-version">
              <span className="version-current">{container.versionFrom}</span>
              <span className="version-arrow">&rarr;</span>
              <span className="version-target">{container.versionTo}</span>
            </div>
          )}
        </div>
      );

    case 'failed':
      return (
        <div className="cr cr-failed">
          <span className="cr-icon"><i className="fa-solid fa-circle-xmark"></i></span>
          <span className="cr-name">{container.name}</span>
          {container.badge && <span className={`container-badge ${container.badge.toLowerCase()}`}>{container.badge}</span>}
          {container.versionFrom && container.versionTo && (
            <div className="cr-version">
              <span className="version-current">{container.versionFrom}</span>
              <span className="version-arrow">&rarr;</span>
              <span className="version-target">{container.versionTo}</span>
            </div>
          )}
          {(container.error || container.message) && (
            <div className="cr-error">{container.error || container.message}</div>
          )}
        </div>
      );
  }
}

function GroupHeader({ label, count, icon, colorClass, collapsed, onToggle }: {
  label: string;
  count: number;
  icon: string;
  colorClass: string;
  collapsed: boolean;
  onToggle: () => void;
}) {
  return (
    <button className={`group-header ${colorClass}`} onClick={onToggle}>
      <i className={`fa-solid fa-chevron-${collapsed ? 'right' : 'down'} group-chevron`}></i>
      <i className={`fa-solid ${icon}`}></i>
      <span className="group-label">{label}</span>
      <span className="group-count">{count}</span>
    </button>
  );
}

export function GroupedContainerList({ containers, expectedDependents, dependentsRestarted, dependentsBlocked, currentStage }: GroupedContainerListProps) {
  const running = containers.filter(c => c.status === 'in_progress');
  const queued = containers.filter(c => c.status === 'pending');
  const failed = containers.filter(c => c.status === 'failed');
  const completed = containers.filter(c => c.status === 'success');

  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});

  // Auto-collapse completed when count > 5
  useEffect(() => {
    if (completed.length > 5 && collapsedGroups['completed'] === undefined) {
      setCollapsedGroups(prev => ({ ...prev, completed: true }));
    }
  }, [completed.length]);

  const toggle = (group: string) => {
    setCollapsedGroups(prev => ({ ...prev, [group]: !prev[group] }));
  };

  const hasDependents = dependentsRestarted.length > 0 || dependentsBlocked.length > 0 ||
    (currentStage === 'restarting_dependents' && expectedDependents.length > 0);

  return (
    <section className="grouped-containers">
      {/* Running */}
      {running.length > 0 && (
        <div className="container-group">
          <GroupHeader
            label="Running"
            count={running.length}
            icon="fa-circle-notch"
            colorClass="group-running"
            collapsed={!!collapsedGroups['running']}
            onToggle={() => toggle('running')}
          />
          {!collapsedGroups['running'] && (
            <div className="group-items">
              {running.map(c => <ContainerRow key={c.name} container={c} />)}
            </div>
          )}
        </div>
      )}

      {/* Queued */}
      {queued.length > 0 && (
        <div className="container-group">
          <GroupHeader
            label="Queued"
            count={queued.length}
            icon="fa-clock"
            colorClass="group-queued"
            collapsed={!!collapsedGroups['queued']}
            onToggle={() => toggle('queued')}
          />
          {!collapsedGroups['queued'] && (
            <div className="group-items">
              {queued.map(c => <ContainerRow key={c.name} container={c} />)}
            </div>
          )}
        </div>
      )}

      {/* Failed */}
      {failed.length > 0 && (
        <div className="container-group">
          <GroupHeader
            label="Failed"
            count={failed.length}
            icon="fa-circle-xmark"
            colorClass="group-failed"
            collapsed={!!collapsedGroups['failed']}
            onToggle={() => toggle('failed')}
          />
          {!collapsedGroups['failed'] && (
            <div className="group-items">
              {failed.map(c => <ContainerRow key={c.name} container={c} />)}
            </div>
          )}
        </div>
      )}

      {/* Completed */}
      {completed.length > 0 && (
        <div className="container-group">
          <GroupHeader
            label="Completed"
            count={completed.length}
            icon="fa-circle-check"
            colorClass="group-completed"
            collapsed={!!collapsedGroups['completed']}
            onToggle={() => toggle('completed')}
          />
          {!collapsedGroups['completed'] && (
            <div className="group-items">
              {completed.map(c => <ContainerRow key={c.name} container={c} />)}
            </div>
          )}
        </div>
      )}

      {/* Dependent Containers */}
      {hasDependents && (
        <div className="container-group">
          <div className="group-header group-dependents">
            <i className="fa-solid fa-link"></i>
            <span className="group-label">Dependent Containers</span>
          </div>
          <div className="group-items">
            {dependentsRestarted.map((depName) => (
              <div key={depName} className="cr cr-success cr-dependent">
                <span className="cr-icon"><i className="fa-solid fa-check"></i></span>
                <span className="cr-name">{depName}</span>
                <span className="container-badge dependent">Dependent</span>
                <div className="cr-result">Restarted successfully</div>
              </div>
            ))}
            {dependentsBlocked.map((depName) => (
              <div key={depName} className="cr cr-failed cr-dependent">
                <span className="cr-icon"><i className="fa-solid fa-xmark"></i></span>
                <span className="cr-name">{depName}</span>
                <span className="container-badge dependent warning">Blocked</span>
                <div className="cr-error">Pre-update check failed</div>
              </div>
            ))}
            {expectedDependents
              .filter(d => !dependentsRestarted.includes(d) && !dependentsBlocked.includes(d))
              .map((depName) => (
              <div key={depName} className="cr cr-running cr-dependent">
                <span className="cr-icon"><i className="fa-solid fa-spinner fa-spin"></i></span>
                <span className="cr-name">{depName}</span>
                <span className="container-badge dependent">Dependent</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
