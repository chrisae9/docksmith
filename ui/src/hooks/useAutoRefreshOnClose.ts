import { useRef, useEffect } from 'react';

/**
 * Custom hook that triggers a callback when a modal/dialog closes
 * Handles close by any means: button click, ESC key, clicking outside, etc.
 * @param isOpen - Whether the modal is currently open
 * @param onRefresh - Callback to execute when modal closes
 */
export function useAutoRefreshOnClose(
  isOpen: boolean,
  onRefresh: () => void
) {
  const wasOpenRef = useRef(false);

  useEffect(() => {
    // If modal was open and is now closed, trigger refresh
    if (wasOpenRef.current && !isOpen) {
      onRefresh();
    }
    wasOpenRef.current = isOpen;
  }, [isOpen, onRefresh]);
}
