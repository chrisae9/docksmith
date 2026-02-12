import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';

interface ActionMenuButtonProps {
  isActive: boolean;
  isLoading?: boolean;
  onClick: (e: React.MouseEvent) => void;
  disabled?: boolean;
  ariaLabel?: string;
}

export function ActionMenuButton({
  isActive,
  isLoading = false,
  onClick,
  disabled = false,
  ariaLabel = 'Actions',
}: ActionMenuButtonProps) {
  return (
    <button
      className={`action-menu-btn ${isActive ? 'active' : ''}`}
      onClick={(e) => {
        e.stopPropagation();
        onClick(e);
      }}
      disabled={disabled || isLoading}
      aria-label={ariaLabel}
      aria-expanded={isActive}
      aria-haspopup="menu"
    >
      {isLoading ? (
        <i className="fa-solid fa-circle-notch fa-spin" aria-hidden="true"></i>
      ) : (
        <i className="fa-solid fa-ellipsis-vertical" aria-hidden="true"></i>
      )}
    </button>
  );
}

interface ActionMenuProps {
  isActive: boolean;
  menuRef?: RefObject<HTMLDivElement | null>;
  children: ReactNode;
}

export function ActionMenu({ isActive, menuRef, children }: ActionMenuProps) {
  const internalRef = useRef<HTMLDivElement>(null);
  const [flipUp, setFlipUp] = useState(false);

  useLayoutEffect(() => {
    if (!isActive) {
      setFlipUp(false);
      return;
    }

    const checkPosition = () => {
      const menu = internalRef.current;
      if (!menu) return;

      const rect = menu.getBoundingClientRect();
      const viewportHeight = window.innerHeight;
      const bottomNavHeight = 70;

      if (rect.bottom > viewportHeight - bottomNavHeight) {
        setFlipUp(true);
      } else {
        setFlipUp(false);
      }
    };

    checkPosition();
  }, [isActive]);

  if (!isActive) return null;

  return (
    <div
      className={`action-menu ${flipUp ? 'flip-up' : ''}`}
      role="menu"
      ref={(node) => {
        (internalRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
        if (menuRef && 'current' in menuRef) {
          (menuRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
        }
      }}
    >
      {children}
    </div>
  );
}

interface ActionMenuItemProps {
  icon: string;
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  title?: string;
}

export function ActionMenuItem({
  icon,
  label,
  onClick,
  danger = false,
  disabled = false,
  title,
}: ActionMenuItemProps) {
  return (
    <button
      className={`${danger ? 'danger' : ''} ${disabled ? 'disabled' : ''}`}
      onClick={disabled ? undefined : onClick}
      disabled={disabled}
      title={title}
      role="menuitem"
    >
      <i className={`fa-solid ${icon}`} aria-hidden="true"></i> {label}
    </button>
  );
}

export function ActionMenuDivider() {
  return <div className="menu-divider"></div>;
}

interface ConfirmRemoveProps {
  onConfirm: () => void;
  onCancel: () => void;
  label?: string;
}

export function ConfirmRemove({
  onConfirm,
  onCancel,
  label = 'Remove?',
}: ConfirmRemoveProps) {
  return (
    <div className="confirm-remove">
      <span>{label}</span>
      <button className="confirm-yes" onClick={onConfirm}>
        Yes
      </button>
      <button className="confirm-no" onClick={onCancel}>
        No
      </button>
    </div>
  );
}
