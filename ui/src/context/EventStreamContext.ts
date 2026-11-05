import { createContext, useContext } from 'react';
import { useEventStreamCore } from '../hooks/useEventStream';

type EventStreamValue = ReturnType<typeof useEventStreamCore>;

export const EventStreamContext = createContext<EventStreamValue | null>(null);

export function useEventStream(): EventStreamValue {
  const ctx = useContext(EventStreamContext);
  if (!ctx) throw new Error('useEventStream must be used within EventStreamProvider');
  return ctx;
}
