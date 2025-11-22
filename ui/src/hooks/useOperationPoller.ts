import { useEffect, useRef } from 'react';

export interface OperationStatus {
  status: 'pending' | 'in_progress' | 'complete' | 'failed';
  error_message?: string;
  [key: string]: any;
}

export interface PollerOptions {
  pollInterval?: number;  // milliseconds between polls
  maxPolls?: number;      // maximum number of polls
  onTimeout?: () => void; // called when polling times out
}

/**
 * Custom hook that polls an operation until it completes or times out
 * @param operationId - The operation ID to poll
 * @param onUpdate - Callback when operation status updates
 * @param options - Polling configuration
 */
export function useOperationPoller(
  operationId: string | null | undefined,
  onUpdate: (operation: OperationStatus) => void,
  options: PollerOptions = {}
) {
  const {
    pollInterval = 5000,
    maxPolls = 60,
    onTimeout
  } = options;

  const pollingRef = useRef(false);
  const abortRef = useRef(false);

  useEffect(() => {
    if (!operationId || pollingRef.current) return;

    pollingRef.current = true;
    abortRef.current = false;

    const poll = async () => {
      let completed = false;
      let pollCount = 0;

      while (!completed && pollCount < maxPolls && !abortRef.current) {
        await new Promise(resolve => setTimeout(resolve, pollInterval));
        pollCount++;

        try {
          const response = await fetch(`/api/operations/${operationId}`);
          const data = await response.json();

          if (data.success && data.data) {
            const operation = data.data;
            onUpdate(operation);

            if (operation.status === 'complete' || operation.status === 'failed') {
              completed = true;
            }
          }
        } catch (error) {
          // Continue polling on network errors
          console.error('Polling error:', error);
        }
      }

      if (!completed && !abortRef.current) {
        // Timeout occurred
        if (onTimeout) {
          onTimeout();
        }
      }

      pollingRef.current = false;
    };

    poll();

    return () => {
      abortRef.current = true;
    };
  }, [operationId, onUpdate, pollInterval, maxPolls, onTimeout]);
}
