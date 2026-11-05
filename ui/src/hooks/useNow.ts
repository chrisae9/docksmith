import { useCallback, useSyncExternalStore } from 'react';

const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;
function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!timer) timer = setInterval(() => { for (const notify of listeners) notify(); }, 1000);
  return () => {
    listeners.delete(listener);
    if (!listeners.size) { clearInterval(timer); timer = undefined; }
  };
}
const snapshot = () => Math.floor(Date.now() / 1000) * 1000;
const serverSnapshot = () => 0;
export function useNow(active = true) {
  const listen = useCallback((listener: () => void) => active ? subscribe(listener) : () => {}, [active]);
  return useSyncExternalStore(listen, snapshot, serverSnapshot);
}
