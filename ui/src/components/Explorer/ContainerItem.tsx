import type { ContainerExplorerItem } from '../../types/api';
import {
  ActionMenuButton,
  ActionMenu,
  ActionMenuItem,
  ActionMenuDivider,
  ConfirmRemove,
  StateIndicator,
} from '../shared';

// Type fix for RefObject
type RefCallback = React.RefObject<HTMLDivElement | null>;

interface ContainerItemProps {
  container: ContainerExplorerItem;
  isActive: boolean;
  isLoading: boolean;
  confirmRemove: boolean;
  showLabels?: boolean;
  onMenuToggle: () => void;
  onAction: (name: string, action: 'start' | 'stop' | 'restart' | 'remove', force?: boolean) => void;
  onNavigateToDetail: (tab?: 'overview' | 'logs' | 'inspect') => void;
  onConfirmRemove: () => void;
  menuRef: RefCallback;
}

export function ContainerItem({
  container,
  isActive,
  isLoading,
  confirmRemove,
  showLabels = false,
  onMenuToggle,
  onAction,
  onNavigateToDetail,
  onConfirmRemove,
  menuRef,
}: ContainerItemProps) {
  const isRunning = container.state === 'running';

  // Format created time as relative time
  const formatCreated = (timestamp: number): string => {
    const now = Date.now() / 1000;
    const diff = now - timestamp;
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    if (diff < 604800) return `${Math.floor(diff / 86400)}d ago`;
    return new Date(timestamp * 1000).toLocaleDateString();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onNavigateToDetail();
    }
  };

  return (
    <li className="explorer-item">
      <StateIndicator
        state={container.state}
        healthStatus={container.health_status}
        isLoading={isLoading}
      />
      <div
        className="item-content"
        onClick={() => onNavigateToDetail()}
        onKeyDown={handleKeyDown}
        tabIndex={0}
        role="button"
        aria-label={`${container.name}, ${container.state}${container.health_status ? `, health: ${container.health_status}` : ''}. Press Enter to view details.`}
      >
        <span className="item-name">{container.name}</span>
        <span className="item-meta">{container.image}</span>
        {showLabels && (
          <span className="item-labels">
            {container.stack && <span className="item-label">Stack: {container.stack}</span>}
            <span className="item-label">Created: {formatCreated(container.created)}</span>
          </span>
        )}
      </div>
      <div className="item-actions" ref={isActive ? menuRef : undefined}>
        <ActionMenuButton
          isActive={isActive}
          isLoading={isLoading}
          onClick={onMenuToggle}
        />
        <ActionMenu isActive={isActive}>
          {isRunning ? (
            <>
              <ActionMenuItem icon="fa-stop" label="Stop" onClick={() => onAction(container.name, 'stop')} />
              <ActionMenuItem icon="fa-rotate" label="Restart" onClick={() => onAction(container.name, 'restart')} />
            </>
          ) : (
            <ActionMenuItem icon="fa-play" label="Start" onClick={() => onAction(container.name, 'start')} />
          )}
          <ActionMenuDivider />
          <ActionMenuItem icon="fa-file-lines" label="Logs" onClick={() => onNavigateToDetail('logs')} />
          <ActionMenuItem icon="fa-magnifying-glass" label="Inspect" onClick={() => onNavigateToDetail('inspect')} />
          <ActionMenuDivider />
          {confirmRemove ? (
            <ConfirmRemove
              onConfirm={() => onAction(container.name, 'remove', true)}
              onCancel={onMenuToggle}
            />
          ) : (
            <ActionMenuItem icon="fa-trash" label="Remove" onClick={onConfirmRemove} danger />
          )}
        </ActionMenu>
      </div>
    </li>
  );
}
