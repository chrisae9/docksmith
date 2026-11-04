import { cloneElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import './Tooltip.css';

export interface TooltipProps {
  label: string;
  placement?: 'top' | 'bottom';
  align?: 'start' | 'center' | 'end';
  children: ReactElement;
}

type TooltipChildProps = {
  className?: string;
  children?: ReactNode;
  'aria-label'?: string;
};

export function Tooltip({ label, placement = 'bottom', align = 'center', children }: TooltipProps) {
  if (!isValidElement(children)) return children;
  const child = children as ReactElement<TooltipChildProps>;
  const props = child.props;
  const className = [props.className, 'tooltip-host'].filter(Boolean).join(' ');

  return cloneElement(child, {
    className,
    'aria-label': label,
    children: (
      <>
        {props.children}
        <span aria-hidden="true" className={`tooltip-bubble tooltip-${placement} tooltip-align-${align}`}>
          {label}
        </span>
      </>
    ),
  });
}
