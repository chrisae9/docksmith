import type { VolumeInfo } from '../../types/api';
import {
  ActionMenuButton,
  ActionMenu,
  ActionMenuItem,
  ConfirmRemove,
} from '../shared';
import { formatSize, isAnonymousVolume, truncateVolumeName } from './utils';

interface VolumeItemProps {
  volume: VolumeInfo;
  isActive: boolean;
  isLoading: boolean;
  confirmRemove: boolean;
  onMenuToggle: () => void;
  onRemove: (force?: boolean) => void;
  onConfirmRemove: () => void;
  onClose: () => void;
}

export function VolumeItem({
  volume,
  isActive,
  isLoading,
  confirmRemove,
  onMenuToggle,
  onRemove,
  onConfirmRemove,
  onClose,
}: VolumeItemProps) {
  const containers = volume.containers || [];
  const inUse = containers.length > 0;
  const anonymous = isAnonymousVolume(volume.name);

  return (
    <li className="explorer-item">
      <i className={`fa-solid fa-hard-drive item-icon ${inUse ? 'in-use' : ''} ${isLoading ? 'loading' : ''}`}></i>
      <div className="item-content">
        <span className="item-name" title={anonymous ? volume.name : undefined}>
          {truncateVolumeName(volume.name)}
        </span>
        <span className="item-meta">
          {volume.driver}
          {volume.size >= 0 && ` \u2022 ${formatSize(volume.size)}`}
          {inUse && ` \u2022 ${containers.length} container${containers.length !== 1 ? 's' : ''}`}
        </span>
      </div>
      <div className="item-badges">
        {!inUse && <span className="badge unused">Unused</span>}
      </div>
      <div className="item-actions">
        <ActionMenuButton
          isActive={isActive}
          isLoading={isLoading}
          onClick={onMenuToggle}
        />
        <ActionMenu isActive={isActive} onClose={onClose}>
          {confirmRemove ? (
            <ConfirmRemove
              onConfirm={() => onRemove(inUse)}
              onCancel={onMenuToggle}
              label={`Remove${inUse ? ' (Force)' : ''}?`}
            />
          ) : (
            <ActionMenuItem
              icon="fa-trash"
              label={`Remove${inUse ? ' (Force)' : ''}`}
              onClick={onConfirmRemove}
              danger
            />
          )}
        </ActionMenu>
      </div>
    </li>
  );
}
