import { useState, useEffect } from 'react';

/**
 * Custom hook that tracks elapsed time from a start time
 * @param startTime - Start time in milliseconds (Date.now() format)
 * @param isActive - Whether the timer should be running
 * @param endTime - Optional end time for completed operations
 * @returns Elapsed time in seconds
 */
export function useElapsedTime(startTime: number | null, isActive: boolean, endTime?: number | null): number {
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!startTime) {
      setElapsed(0);
      return;
    }

    // If we have an end time, show final elapsed
    if (endTime) {
      setElapsed(Math.floor((endTime - startTime) / 1000));
      return;
    }

    // If not active and no end time, show 0
    if (!isActive) {
      setElapsed(0);
      return;
    }

    setElapsed(Math.floor((Date.now() - startTime) / 1000));
    const interval = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startTime) / 1000));
    }, 1000);
    return () => clearInterval(interval);
  }, [startTime, isActive, endTime]);

  return elapsed;
}
