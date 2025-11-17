import { useEffect, useRef, useState, useCallback } from 'react';

export interface UpdateProgressEvent {
  operation_id: string;
  container_name: string;
  stack_name: string;
  stage: string;
  percent: number;
  progress: number; // Backend sends "progress", we map to percent
  message: string;
  timestamp?: number;
}

export interface CheckProgressEvent {
  stage: string;
  total: number;
  checked: number;
  percent: number;
  container_name: string;
  message: string;
}

export interface EventStreamState {
  connected: boolean;
  events: UpdateProgressEvent[];
  lastEvent: UpdateProgressEvent | null;
  checkProgress: CheckProgressEvent | null;
}

export function useEventStream(enabled: boolean = true) {
  const [state, setState] = useState<EventStreamState>({
    connected: false,
    events: [],
    lastEvent: null,
    checkProgress: null,
  });

  const eventSourceRef = useRef<EventSource | null>(null);

  const connect = useCallback(() => {
    if (!enabled || eventSourceRef.current) return;

    const eventSource = new EventSource('/api/events');
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      console.log('SSE connected');
      setState(prev => ({ ...prev, connected: true }));
    };

    eventSource.onerror = (err) => {
      console.error('SSE error:', err);
      setState(prev => ({ ...prev, connected: false }));

      // Try to reconnect after 3 seconds
      setTimeout(() => {
        eventSourceRef.current?.close();
        eventSourceRef.current = null;
        connect();
      }, 3000);
    };

    // Listen for connection event
    eventSource.addEventListener('connected', (e) => {
      console.log('SSE connected event:', e.data);
      setState(prev => ({ ...prev, connected: true }));
    });

    // Listen for update progress events
    eventSource.addEventListener('update.progress', (e) => {
      try {
        const data = JSON.parse(e.data);
        const progressEvent: UpdateProgressEvent = data.payload;

        console.log('Update progress:', progressEvent);

        setState(prev => ({
          ...prev,
          events: [...prev.events.slice(-99), progressEvent], // Keep last 100 events
          lastEvent: progressEvent,
        }));
      } catch (err) {
        console.error('Error parsing progress event:', err);
      }
    });

    // Listen for container updated events
    eventSource.addEventListener('container.updated', (e) => {
      console.log('Container updated:', e.data);
    });

    // Listen for check progress events
    eventSource.addEventListener('check.progress', (e) => {
      try {
        const data = JSON.parse(e.data);
        const checkEvent: CheckProgressEvent = data.payload;

        console.log('Check progress:', checkEvent);

        setState(prev => ({
          ...prev,
          checkProgress: checkEvent,
        }));
      } catch (err) {
        console.error('Error parsing check progress event:', err);
      }
    });
  }, [enabled]);

  const disconnect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
      setState(prev => ({ ...prev, connected: false }));
    }
  }, []);

  const clearEvents = useCallback(() => {
    setState(prev => ({ ...prev, events: [], lastEvent: null }));
  }, []);

  useEffect(() => {
    if (enabled) {
      connect();
    } else {
      disconnect();
    }

    return () => {
      disconnect();
    };
  }, [enabled, connect, disconnect]);

  return {
    ...state,
    clearEvents,
    reconnect: connect,
  };
}
