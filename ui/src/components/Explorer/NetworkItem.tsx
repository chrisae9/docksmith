import type { NetworkInfo } from '../../types/api';
import {
  ActionMenuButton,
  ActionMenu,
  ActionMenuItem,
  ConfirmRemove,
} from '../shared';

// Type fix for RefObject
type RefCallback = React.RefObject<HTMLDivElement | null>;

interface NetworkItemProps {
  network: NetworkInfo;
  isActive: boolean;
  isLoading: boolean;
  confirmRemove: boolean;
  onMenuToggle: () => void;
  onRemove: () => void;
  onConfirmRemove: () => void;
  menuRef: RefCallback;
}

export function NetworkItem({
  network,
  isActive,
  isLoading,
  confirmRemove,
  onMenuToggle,
  onRemove,
  onConfirmRemove,
  menuRef,
}: NetworkItemProps) {
  const containers = network.containers || [];
  const canRemove = !network.is_default && containers.length === 0;

  return (
    <li className="explorer-item">
      <i className={`fa-solid fa-network-wired item-icon ${network.is_default ? 'default' : ''} ${isLoading ? 'loading' : ''}`}></i>
      <div className="item-content">
        <span className="item-name">{network.name}</span>
        <span className="item-meta">
          {network.driver}
          {containers.length > 0 && ` \u2022 ${containers.length} container${containers.length !== 1 ? 's' : ''}`}
        </span>
      </div>
      <div className="item-badges">
        {network.is_default && <span className="badge default">Default</span>}
      </div>
      {!network.is_default && (
        <div className="item-actions" ref={isActive ? menuRef : undefined}>
          <ActionMenuButton
            isActive={isActive}
            isLoading={isLoading}
            onClick={onMenuToggle}
          />
          <ActionMenu isActive={isActive}>
            {confirmRemove ? (
              <ConfirmRemove
                onConfirm={onRemove}
                onCancel={onMenuToggle}
              />
            ) : (
              <ActionMenuItem
                icon="fa-trash"
                label="Remove"
                onClick={onConfirmRemove}
                danger
                disabled={!canRemove}
                title={!canRemove ? 'Cannot remove: network has connected containers' : undefined}
              />
            )}
          </ActionMenu>
        </div>
      )}
    </li>
  );
}
