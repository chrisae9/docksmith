/**
 * Formats a timestamp into a human-readable "time ago" string
 * @param timestamp ISO 8601 timestamp string
 * @returns Formatted string like "5m ago", "2h ago", "3d ago"
 */
export function formatTimeAgo(timestamp: string): string {
  if (!timestamp) return 'Never';

  const now = Date.now();
  const then = new Date(timestamp).getTime();

  // Handle invalid dates
  if (isNaN(then)) return 'Invalid date';

  const diffMs = now - then;

  // Handle future dates or negative differences
  if (diffMs < 0) return 'Just now';

  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHr = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHr / 24);

  if (diffDay > 0) return `${diffDay}d ago`;
  if (diffHr > 0) return `${diffHr}h ago`;
  if (diffMin > 0) return `${diffMin}m ago`;
  if (diffSec > 0) return `${diffSec}s ago`;
  return 'Just now';
}

/**
 * Formats a timestamp with additional date information for older times
 * @param timeStr ISO 8601 timestamp string
 * @returns Formatted string with relative time or full date for older entries
 */
export function formatTimeWithDate(timeStr?: string): string {
  if (!timeStr) return 'Unknown';

  const date = new Date(timeStr);

  // Handle invalid dates
  if (isNaN(date.getTime())) return 'Invalid date';

  const now = new Date();
  const diffMs = now.getTime() - date.getTime();

  // Handle future dates or negative differences
  if (diffMs < 0) return 'Just now';

  const diffMins = Math.floor(diffMs / (1000 * 60));
  const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;

  // For older dates, show full date
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}
