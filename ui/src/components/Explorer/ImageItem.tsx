import type { ImageInfo } from '../../types/api';
import {
  ActionMenuButton,
  ActionMenu,
  ActionMenuItem,
  ConfirmRemove,
} from '../shared';
import { formatSize, formatRelativeTime, truncateId } from './utils';

// Type fix for RefObject
type RefCallback = React.RefObject<HTMLDivElement | null>;

interface ImageItemProps {
  image: ImageInfo;
  isActive: boolean;
  isLoading: boolean;
  confirmRemove: boolean;
  onMenuToggle: () => void;
  onRemove: (force?: boolean) => void;
  onConfirmRemove: () => void;
  menuRef: RefCallback;
}

export function ImageItem({
  image,
  isActive,
  isLoading,
  confirmRemove,
  onMenuToggle,
  onRemove,
  onConfirmRemove,
  menuRef,
}: ImageItemProps) {
  const tags = image.tags || [];
  const primaryTag = tags.length > 0 ? tags[0] : `<none> (${truncateId(image.id)})`;

  return (
    <li className={`explorer-item ${image.dangling ? 'dangling' : ''}`}>
      <i className={`fa-solid fa-cube item-icon ${image.in_use ? 'in-use' : ''} ${isLoading ? 'loading' : ''}`}></i>
      <div className="item-content">
        <span className="item-name">{primaryTag}</span>
        <span className="item-meta">
          {formatSize(image.size)} &bull; {formatRelativeTime(image.created)}
        </span>
      </div>
      <div className="item-badges">
        {image.in_use && <span className="badge in-use">In Use</span>}
        {image.dangling && <span className="badge dangling">Dangling</span>}
      </div>
      <div className="item-actions" ref={isActive ? menuRef : undefined}>
        <ActionMenuButton
          isActive={isActive}
          isLoading={isLoading}
          onClick={onMenuToggle}
        />
        <ActionMenu isActive={isActive}>
          {confirmRemove ? (
            <ConfirmRemove
              onConfirm={() => onRemove(image.in_use)}
              onCancel={onMenuToggle}
            />
          ) : (
            <ActionMenuItem
              icon="fa-trash"
              label={`Remove${image.in_use ? ' (Force)' : ''}`}
              onClick={onConfirmRemove}
              danger
            />
          )}
        </ActionMenu>
      </div>
    </li>
  );
}
