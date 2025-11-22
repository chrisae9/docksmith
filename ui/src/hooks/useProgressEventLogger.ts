import { useEffect } from 'react';
import type { UpdateProgressEvent } from './useEventStream';

export interface LogEntry {
  time: number;
  message: string;
}

export interface ProgressStateWithLogs {
  logs: LogEntry[];
  [key: string]: any;
}

/**
 * Custom hook that automatically adds SSE progress events to a progress state's log array
 * @param progressEvent - The SSE progress event from useEventStream
 * @param progressState - The current progress state (must have a 'logs' array)
 * @param setProgressState - State setter function
 */
export function useProgressEventLogger<T extends ProgressStateWithLogs>(
  progressEvent: UpdateProgressEvent | null,
  progressState: T | null,
  setProgressState: React.Dispatch<React.SetStateAction<T | null>>
) {
  useEffect(() => {
    if (!progressEvent || !progressState) return;

    setProgressState(prev => {
      if (!prev) return prev;

      const newLog: LogEntry = {
        time: progressEvent.timestamp ? progressEvent.timestamp * 1000 : Date.now(),
        message: `${progressEvent.container_name}: ${progressEvent.message}`,
      };

      // Avoid duplicate logs
      const lastLog = prev.logs[prev.logs.length - 1];
      if (lastLog && lastLog.message === newLog.message) {
        return prev;
      }

      return {
        ...prev,
        logs: [...prev.logs.slice(-19), newLog], // Keep last 20 logs
      };
    });
  }, [progressEvent, progressState, setProgressState]);
}
