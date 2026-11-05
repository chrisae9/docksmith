import { useNow } from './useNow';

export function useElapsedTime(startTime: number | null, isActive: boolean, endTime?: number | null): number {
  const now = useNow(!!startTime && isActive && !endTime);
  if (!startTime) return 0;
  if (endTime) return Math.floor((endTime - startTime) / 1000);
  return isActive ? Math.max(0, Math.floor((now - startTime) / 1000)) : 0;
}
