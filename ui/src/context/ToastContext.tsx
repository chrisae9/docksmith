import React, { createContext, useContext, useState, useCallback, useRef } from 'react';

export type ToastType = 'success' | 'error' | 'warning' | 'info';

export interface Toast {
  id: string;
  type: ToastType;
  message: string;
  title?: string;
  duration?: number;
  action?: {
    label: string;
    onClick: () => void;
  };
}

interface ToastContextValue {
  toasts: Toast[];
  addToast: (toast: Omit<Toast, 'id'>) => string;
  removeToast: (id: string) => void;
  clearToasts: () => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

const DEFAULT_DURATION = 4000;
const ERROR_DURATION = 6000;

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastIdRef = useRef(0);
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const removeToast = useCallback((id: string) => {
    // Clear the timer if it exists
    const timer = timersRef.current.get(id);
    if (timer) {
      clearTimeout(timer);
      timersRef.current.delete(id);
    }
    setToasts(prev => prev.filter(t => t.id !== id));
  }, []);

  const addToast = useCallback((toast: Omit<Toast, 'id'>) => {
    const id = `toast-${++toastIdRef.current}`;
    const duration = toast.duration ?? (toast.type === 'error' ? ERROR_DURATION : DEFAULT_DURATION);

    setToasts(prev => {
      // Limit to 5 toasts max, remove oldest if needed
      const newToasts = prev.length >= 5 ? prev.slice(1) : prev;
      return [...newToasts, { ...toast, id }];
    });

    if (duration > 0) {
      const timer = setTimeout(() => {
        removeToast(id);
      }, duration);
      timersRef.current.set(id, timer);
    }

    return id;
  }, [removeToast]);

  const clearToasts = useCallback(() => {
    // Clear all timers
    timersRef.current.forEach(timer => clearTimeout(timer));
    timersRef.current.clear();
    setToasts([]);
  }, []);

  return (
    <ToastContext.Provider value={{ toasts, addToast, removeToast, clearToasts }}>
      {children}
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }

  const { addToast, removeToast, clearToasts, toasts } = context;

  return {
    toasts,
    addToast,
    removeToast,
    clearToasts,
    // Convenience methods
    success: (message: string, opts?: Partial<Omit<Toast, 'id' | 'type' | 'message'>>) =>
      addToast({ type: 'success', message, ...opts }),
    error: (message: string, opts?: Partial<Omit<Toast, 'id' | 'type' | 'message'>>) =>
      addToast({ type: 'error', message, ...opts }),
    warning: (message: string, opts?: Partial<Omit<Toast, 'id' | 'type' | 'message'>>) =>
      addToast({ type: 'warning', message, ...opts }),
    info: (message: string, opts?: Partial<Omit<Toast, 'id' | 'type' | 'message'>>) =>
      addToast({ type: 'info', message, ...opts }),
  };
}
