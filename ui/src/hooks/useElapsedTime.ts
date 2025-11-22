import { useState, useEffect } from 'react';

/**
 * Custom hook that tracks elapsed time from a start time
 * @param startTime - Start time in milliseconds (Date.now() format)
 * @param isActive - Whether the timer should be running
 * @returns Elapsed time in seconds
 */
export function useElapsedTime(startTime: number | null, isActive: boolean): number {
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!isActive || !startTime) {
      setElapsed(0);
      return;
    }

    // Set initial elapsed time
    setElapsed(Math.floor((Date.now() - startTime) / 1000));

    // Update every second
    const interval = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startTime) / 1000));
    }, 1000);

    return () => clearInterval(interval);
  }, [startTime, isActive]);

  return elapsed;
}
