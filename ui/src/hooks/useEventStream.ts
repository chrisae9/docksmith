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

export interface ContainerUpdatedEvent {
  container_id?: string;
  container_name?: string;
  operation_id?: string;
  status?: string;
  source?: string;
  timestamp?: number;
}

export interface EventStreamState {
  connected: boolean;
  reconnecting: boolean;
  reconnectAttempt: number;
  wasDisconnected: boolean; // Track if we recovered from a disconnection
  events: UpdateProgressEvent[];
  lastEvent: UpdateProgressEvent | null;
  checkProgress: CheckProgressEvent | null;
  containerUpdated: ContainerUpdatedEvent | null; // Last container update event with full details
}

export function useEventStream(enabled: boolean = true) {
  const [state, setState] = useState<EventStreamState>({
    connected: false,
    reconnecting: false,
    reconnectAttempt: 0,
    wasDisconnected: false,
    events: [],
    lastEvent: null,
    checkProgress: null,
    containerUpdated: null,
  });

  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hadConnectionRef = useRef(false);
  const reconnectAttemptRef = useRef(0);

  const connect = useCallback(() => {
    if (!enabled || eventSourceRef.current) return;

    const eventSource = new EventSource('/api/events');
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      const wasDisconnected = hadConnectionRef.current;
      hadConnectionRef.current = true;
      reconnectAttemptRef.current = 0; // Reset attempt counter on successful connection
      setState(prev => ({
        ...prev,
        connected: true,
        reconnecting: false,
        reconnectAttempt: 0,
        wasDisconnected, // Signal that we recovered from a disconnection
      }));
    };

    eventSource.onerror = () => {
      setState(prev => ({
        ...prev,
        connected: false,
        reconnecting: true,
        wasDisconnected: false,
      }));

      // Exponential backoff: 1s, 2s, 4s, 8s, max 15s
      // Use ref to avoid stale closure issue with state
      const attempt = reconnectAttemptRef.current;
      const delay = Math.min(1000 * Math.pow(2, attempt), 15000);

      // Clear any existing timeout
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }

      reconnectTimeoutRef.current = setTimeout(() => {
        eventSourceRef.current?.close();
        eventSourceRef.current = null;
        reconnectAttemptRef.current += 1; // Increment ref
        setState(prev => ({ ...prev, reconnectAttempt: reconnectAttemptRef.current }));
        connect();
      }, delay);
    };

    // Listen for connection event
    eventSource.addEventListener('connected', () => {
      setState(prev => ({ ...prev, connected: true }));
    });

    // Listen for update progress events
    eventSource.addEventListener('update.progress', (e) => {
      try {
        const data = JSON.parse(e.data);
        const progressEvent: UpdateProgressEvent = data.payload;

        setState(prev => ({
          ...prev,
          events: [...prev.events.slice(-99), progressEvent], // Keep last 100 events
          lastEvent: progressEvent,
        }));
      } catch {
        // Silently ignore parsing errors
      }
    });

    // Listen for container updated events
    eventSource.addEventListener('container.updated', (e) => {
      try {
        const data = JSON.parse(e.data);
        const event: ContainerUpdatedEvent = {
          ...data.payload,
          timestamp: data.payload?.timestamp || Date.now(),
        };

        setState(prev => ({
          ...prev,
          containerUpdated: event,
        }));
      } catch {
        // Silently ignore parsing errors - still trigger with minimal event
        setState(prev => ({
          ...prev,
          containerUpdated: { timestamp: Date.now() },
        }));
      }
    });

    // Listen for check progress events
    eventSource.addEventListener('check.progress', (e) => {
      try {
        const data = JSON.parse(e.data);
        const checkEvent: CheckProgressEvent = data.payload;

        setState(prev => ({
          ...prev,
          checkProgress: checkEvent,
        }));
      } catch {
        // Silently ignore parsing errors
      }
    });
  }, [enabled]);

  const disconnect = useCallback(() => {
    // Clear any pending reconnect timeout
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
      setState(prev => ({ ...prev, connected: false, reconnecting: false }));
    }
  }, []);

  const clearEvents = useCallback(() => {
    setState(prev => ({ ...prev, events: [], lastEvent: null }));
  }, []);

  const clearWasDisconnected = useCallback(() => {
    setState(prev => ({ ...prev, wasDisconnected: false }));
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
    clearWasDisconnected,
    reconnect: connect,
  };
}
