import { useToast, type Toast as ToastType } from '../../context/ToastContext';
import './Toast.css';

const ICONS: Record<ToastType['type'], string> = {
  success: 'fa-check-circle',
  error: 'fa-exclamation-circle',
  warning: 'fa-exclamation-triangle',
  info: 'fa-info-circle',
};

function ToastItem({ toast, onDismiss }: { toast: ToastType; onDismiss: () => void }) {
  return (
    <div className={`toast toast-${toast.type}`} role="alert">
      <i className={`toast-icon fa-solid ${ICONS[toast.type]}`} />
      <div className="toast-content">
        {toast.title && <div className="toast-title">{toast.title}</div>}
        <div className="toast-message">{toast.message}</div>
      </div>
      {toast.action && (
        <button className="toast-action" onClick={toast.action.onClick}>
          {toast.action.label}
        </button>
      )}
      <button className="toast-dismiss" onClick={onDismiss} aria-label="Dismiss">
        <i className="fa-solid fa-xmark" />
      </button>
    </div>
  );
}

export function ToastContainer() {
  const { toasts, removeToast } = useToast();

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container" aria-live="polite">
      {toasts.map(toast => (
        <ToastItem
          key={toast.id}
          toast={toast}
          onDismiss={() => removeToast(toast.id)}
        />
      ))}
    </div>
  );
}
