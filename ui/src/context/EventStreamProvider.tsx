import type { ReactNode } from 'react';
import { useEventStreamCore } from '../hooks/useEventStream';
import { EventStreamContext } from './EventStreamContext';

export function EventStreamProvider({ children }: { children: ReactNode }) {
  const value = useEventStreamCore();
  return (
    <EventStreamContext.Provider value={value}>
      {children}
    </EventStreamContext.Provider>
  );
}
