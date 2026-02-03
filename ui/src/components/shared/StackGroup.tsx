import type { ReactNode } from 'react';

interface StackGroupProps {
  name: string;
  isCollapsed: boolean;
  onToggle: () => void;
  count?: number;
  hasUpdates?: boolean;
  isStandalone?: boolean;
  icon?: string;
  children: ReactNode;
  actions?: ReactNode;
  listClassName?: string;
}

export function StackGroup({
  name,
  isCollapsed,
  onToggle,
  count,
  hasUpdates = false,
  isStandalone = false,
  icon,
  children,
  actions,
  listClassName = 'stack-container-list',
}: StackGroupProps) {
  const defaultIcon = isStandalone ? 'fa-cube' : 'fa-layer-group';
  const stackIcon = icon || defaultIcon;

  return (
    <div className="stack-group">
      {/* Separate header container to avoid nested buttons */}
      <div className={`stack-header ${isStandalone ? 'standalone' : ''}`}>
        <button
          className="stack-toggle"
          onClick={onToggle}
          aria-expanded={!isCollapsed}
          aria-label={`${isCollapsed ? 'Expand' : 'Collapse'} ${name}`}
        >
          <i
            className={`fa-solid fa-chevron-${isCollapsed ? 'right' : 'down'} toggle-icon`}
            aria-hidden="true"
          ></i>
          {stackIcon && (
            <i className={`fa-solid ${stackIcon} stack-icon`} aria-hidden="true"></i>
          )}
          <span className="stack-name">{name}</span>
          {hasUpdates && <span className="badge-dot" aria-label="Has updates"></span>}
          {count !== undefined && (
            <span className="stack-count">{count}</span>
          )}
        </button>
        {/* Actions rendered outside the toggle button to avoid nested interactive elements */}
        {actions && (
          <div className="stack-actions-container" onClick={(e) => e.stopPropagation()}>
            {actions}
          </div>
        )}
      </div>
      {!isCollapsed && (
        <ul className={listClassName}>
          {children}
        </ul>
      )}
    </div>
  );
}
