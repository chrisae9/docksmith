import { useEffect, useRef } from 'react';

/**
 * Custom hook that triggers periodic background refresh
 * @param refreshFn - Function to call for refresh
 * @param intervalMs - Refresh interval in milliseconds (default: 60000 = 1 minute)
 * @param enabled - Whether periodic refresh is enabled (default: true)
 * @param shouldSkip - Optional function to determine if refresh should be skipped
 */
export function usePeriodicRefresh(
  refreshFn: () => void | Promise<void>,
  intervalMs: number = 60000,
  enabled: boolean = true,
  shouldSkip?: () => boolean
) {
  const intervalRef = useRef<number | null>(null);
  const refreshFnRef = useRef(refreshFn);

  // Keep refreshFn ref up to date
  useEffect(() => {
    refreshFnRef.current = refreshFn;
  }, [refreshFn]);

  useEffect(() => {
    if (!enabled) {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    // Set up periodic refresh
    intervalRef.current = window.setInterval(() => {
      // Skip refresh if shouldSkip returns true
      if (shouldSkip && shouldSkip()) {
        return;
      }

      // Call refresh function
      refreshFnRef.current();
    }, intervalMs);

    // Cleanup on unmount or when dependencies change
    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [intervalMs, enabled, shouldSkip]);
}
