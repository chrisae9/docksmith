interface OperationFooterProps {
  isComplete: boolean;
  canForceRetry: boolean;
  canRetry: boolean;
  forceRetryMessage: string;
  forceButtonLabel: string;
  showRollback: boolean;
  onForceRetry: () => void;
  onRetry: () => void;
  onBack: () => void;
  onRollback?: () => void;
}

export function OperationFooter({
  isComplete,
  canForceRetry,
  canRetry,
  forceRetryMessage,
  forceButtonLabel,
  showRollback,
  onForceRetry,
  onRetry,
  onBack,
  onRollback,
}: OperationFooterProps) {
  if (canForceRetry) {
    return (
      <footer className="page-footer">
        <div className="footer-buttons">
          <button
            className="button button-secondary"
            onClick={onBack}
          >
            Cancel
          </button>
          <button
            className="button button-danger"
            onClick={onForceRetry}
            title={forceRetryMessage}
          >
            <i className="fa-solid fa-triangle-exclamation"></i>
            {forceButtonLabel}
          </button>
        </div>
      </footer>
    );
  }

  if (canRetry) {
    return (
      <footer className="page-footer">
        <div className="footer-buttons">
          <button
            className="button button-secondary"
            onClick={onBack}
          >
            Back
          </button>
          <button
            className="button button-primary"
            onClick={onRetry}
          >
            <i className="fa-solid fa-rotate-right"></i>
            Retry
          </button>
        </div>
      </footer>
    );
  }

  if (showRollback && onRollback) {
    return (
      <footer className="page-footer">
        <div className="footer-buttons">
          <button
            className="button button-secondary"
            onClick={onRollback}
          >
            <i className="fa-solid fa-rotate-left"></i>
            Rollback
          </button>
          <button
            className="button button-primary"
            onClick={onBack}
          >
            Done
          </button>
        </div>
      </footer>
    );
  }

  return (
    <footer className="page-footer">
      <button
        className="button button-primary"
        onClick={onBack}
        disabled={!isComplete}
        style={{ width: '100%' }}
      >
        {isComplete ? 'Done' : 'Processing...'}
      </button>
    </footer>
  );
}
