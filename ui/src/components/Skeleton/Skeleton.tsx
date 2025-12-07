import './Skeleton.css';

interface SkeletonProps {
  width?: string | number;
  height?: string | number;
  variant?: 'text' | 'circular' | 'rounded' | 'rectangular';
  className?: string;
  style?: React.CSSProperties;
}

/**
 * Base Skeleton component with shimmer animation
 */
export function Skeleton({
  width = '100%',
  height = '1em',
  variant = 'text',
  className = '',
  style,
}: SkeletonProps) {
  const computedStyle: React.CSSProperties = {
    width: typeof width === 'number' ? `${width}px` : width,
    height: typeof height === 'number' ? `${height}px` : height,
    ...style,
  };

  return (
    <div
      className={`skeleton skeleton-${variant} ${className}`}
      style={computedStyle}
      aria-hidden="true"
    />
  );
}

/**
 * Skeleton for a container row in the list
 */
export function SkeletonContainerRow() {
  return (
    <div className="skeleton-container-row">
      <Skeleton variant="circular" width={22} height={22} />
      <div className="skeleton-container-info">
        <Skeleton variant="text" width="55%" height={17} />
        <Skeleton variant="text" width="35%" height={13} />
      </div>
      <Skeleton variant="circular" width={10} height={10} />
    </div>
  );
}

/**
 * Skeleton for multiple container rows
 */
export function SkeletonContainerList({ count = 5 }: { count?: number }) {
  return (
    <div className="skeleton-container-list">
      {Array.from({ length: count }).map((_, i) => (
        <SkeletonContainerRow key={i} />
      ))}
    </div>
  );
}

/**
 * Skeleton for a stack section (header + list)
 */
export function SkeletonStackSection({ rows = 3 }: { rows?: number }) {
  return (
    <div className="skeleton-stack-section">
      <div className="skeleton-stack-header">
        <Skeleton variant="text" width={80} height={13} />
      </div>
      <SkeletonContainerList count={rows} />
    </div>
  );
}

/**
 * Skeleton for the stats bar
 */
export function SkeletonStatsBar() {
  return (
    <div className="skeleton-stats-bar">
      <div className="skeleton-stat">
        <Skeleton variant="text" width={32} height={24} />
        <Skeleton variant="text" width={48} height={11} />
      </div>
      <div className="skeleton-stat">
        <Skeleton variant="text" width={32} height={24} />
        <Skeleton variant="text" width={48} height={11} />
      </div>
      <div className="skeleton-stat">
        <Skeleton variant="text" width={32} height={24} />
        <Skeleton variant="text" width={48} height={11} />
      </div>
    </div>
  );
}

/**
 * Full dashboard skeleton for initial load
 * Note: Search bar and filter toolbar are rendered in Dashboard loading state
 */
export function SkeletonDashboard() {
  return (
    <div className="skeleton-dashboard">
      <SkeletonStackSection rows={4} />
      <SkeletonStackSection rows={3} />
      <SkeletonStackSection rows={2} />
    </div>
  );
}

/**
 * Skeleton for detail page sections
 */
export function SkeletonDetailSection() {
  return (
    <div className="skeleton-detail-section">
      <Skeleton variant="text" width={120} height={13} className="skeleton-section-title" />
      <div className="skeleton-detail-card">
        <div className="skeleton-detail-row">
          <Skeleton variant="text" width="30%" height={14} />
          <Skeleton variant="text" width="50%" height={14} />
        </div>
        <div className="skeleton-detail-row">
          <Skeleton variant="text" width="25%" height={14} />
          <Skeleton variant="text" width="60%" height={14} />
        </div>
        <div className="skeleton-detail-row">
          <Skeleton variant="text" width="35%" height={14} />
          <Skeleton variant="text" width="40%" height={14} />
        </div>
      </div>
    </div>
  );
}
