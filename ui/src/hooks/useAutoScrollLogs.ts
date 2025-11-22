import { useRef, useEffect } from 'react';

/**
 * Custom hook that creates a ref that automatically scrolls to bottom when content changes
 * @param logs - Array of log entries (or any array that triggers scroll on change)
 * @returns Ref to attach to the scrollable container
 */
export function useAutoScrollLogs<T>(logs: T[] | undefined) {
  const logEntriesRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (logEntriesRef.current && logs) {
      logEntriesRef.current.scrollTop = logEntriesRef.current.scrollHeight;
    }
  }, [logs]);

  return logEntriesRef;
}
