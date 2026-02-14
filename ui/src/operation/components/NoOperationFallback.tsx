import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { getOperations } from '../../api/client';
import { formatOpType, getStatusIcon, getStatusClass } from '../utils';

export function NoOperationFallback() {
  const navigate = useNavigate();
  const [, setSearchParams] = useSearchParams();
  const [recentOps, setRecentOps] = useState<Array<{
    operation_id: string;
    container_name: string;
    operation_type: string;
    status: string;
    batch_group_id?: string;
    started_at?: string;
    batch_details?: Array<{ container_name: string }>;
  }>>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getOperations({ limit: 10 }).then(res => {
      if (res.success && res.data?.operations) {
        // Show in-progress first, then recent completed/failed
        const ops = res.data.operations;
        // Deduplicate by batch_group_id (show one entry per batch)
        const seen = new Set<string>();
        const deduped = ops.filter((op: any) => {
          if (op.batch_group_id) {
            if (seen.has(op.batch_group_id)) return false;
            seen.add(op.batch_group_id);
          }
          return true;
        });
        setRecentOps(deduped.slice(0, 5));
      }
    }).finally(() => setLoading(false));
  }, []);

  const resumeOp = (op: typeof recentOps[0]) => {
    if (op.batch_group_id) {
      setSearchParams({ group: op.batch_group_id }, { replace: true });
    } else {
      setSearchParams({ id: op.operation_id }, { replace: true });
    }
    // Force re-render by navigating to same page
    window.location.reload();
  };

  const inProgressOps = recentOps.filter(op => op.status === 'in_progress' || op.status === 'pending_restart');
  const completedOps = recentOps.filter(op => op.status !== 'in_progress' && op.status !== 'pending_restart');

  return (
    <div className="progress-page operation-progress-page">
      <header className="page-header">
        <button className="back-button" onClick={() => navigate('/')}>
          &larr; Back
        </button>
        <h1>Operations</h1>
        <div className="header-spacer" />
      </header>
      <main className="page-content">
        {loading ? (
          <div className="error-state">
            <i className="fa-solid fa-spinner fa-spin"></i>
            <p>Loading recent operations...</p>
          </div>
        ) : inProgressOps.length > 0 ? (
          <div className="recent-operations">
            <h3><i className="fa-solid fa-spinner fa-spin"></i> In Progress</h3>
            {inProgressOps.map(op => (
              <div key={op.operation_id} className="recent-op-row">
                <div className="recent-op-info">
                  <span className={`recent-op-status ${getStatusClass(op.status)}`}>
                    <i className={`fa-solid ${getStatusIcon(op.status)}`}></i>
                  </span>
                  <span className="recent-op-type">{formatOpType(op.operation_type)}</span>
                  <span className="recent-op-name">
                    {op.batch_details && op.batch_details.length > 1
                      ? `${op.batch_details.length} containers`
                      : op.container_name}
                  </span>
                </div>
                <button className="button button-primary button-sm" onClick={() => resumeOp(op)}>
                  Resume
                </button>
              </div>
            ))}
            {completedOps.length > 0 && (
              <>
                <h3 style={{ marginTop: '1.5rem' }}>Recent</h3>
                {completedOps.map(op => (
                  <div key={op.operation_id} className="recent-op-row">
                    <div className="recent-op-info">
                      <span className={`recent-op-status ${getStatusClass(op.status)}`}>
                        <i className={`fa-solid ${getStatusIcon(op.status)}`}></i>
                      </span>
                      <span className="recent-op-type">{formatOpType(op.operation_type)}</span>
                      <span className="recent-op-name">
                        {op.batch_details && op.batch_details.length > 1
                          ? `${op.batch_details.length} containers`
                          : op.container_name}
                      </span>
                    </div>
                    <button className="button button-secondary button-sm" onClick={() => resumeOp(op)}>
                      View
                    </button>
                  </div>
                ))}
              </>
            )}
          </div>
        ) : recentOps.length > 0 ? (
          <div className="recent-operations">
            <h3>Recent Operations</h3>
            {recentOps.map(op => (
              <div key={op.operation_id} className="recent-op-row">
                <div className="recent-op-info">
                  <span className={`recent-op-status ${getStatusClass(op.status)}`}>
                    <i className={`fa-solid ${getStatusIcon(op.status)}`}></i>
                  </span>
                  <span className="recent-op-type">{formatOpType(op.operation_type)}</span>
                  <span className="recent-op-name">
                    {op.batch_details && op.batch_details.length > 1
                      ? `${op.batch_details.length} containers`
                      : op.container_name}
                  </span>
                </div>
                <button className="button button-secondary button-sm" onClick={() => resumeOp(op)}>
                  View
                </button>
              </div>
            ))}
          </div>
        ) : (
          <div className="error-state">
            <i className="fa-solid fa-inbox"></i>
            <p>No recent operations</p>
            <button className="button button-primary" onClick={() => navigate('/')}>
              Return to Containers
            </button>
          </div>
        )}
      </main>
    </div>
  );
}
