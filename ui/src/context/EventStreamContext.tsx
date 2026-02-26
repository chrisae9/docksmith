import { createContext, useContext, type ReactNode } from 'react';
import { useEventStreamCore } from '../hooks/useEventStream';

type EventStreamValue = ReturnType<typeof useEventStreamCore>;

const EventStreamContext = createContext<EventStreamValue | null>(null);

export function EventStreamProvider({ children }: { children: ReactNode }) {
  const value = useEventStreamCore();
  return (
    <EventStreamContext.Provider value={value}>
      {children}
    </EventStreamContext.Provider>
  );
}

export function useEventStream(): EventStreamValue {
  const ctx = useContext(EventStreamContext);
  if (!ctx) throw new Error('useEventStream must be used within EventStreamProvider');
  return ctx;
}
