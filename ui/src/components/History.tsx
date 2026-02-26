import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getOperations, rollbackContainers } from '../api/client';
import type { UpdateOperation, BatchContainerDetail } from '../types/api';
import { ChangeType, getChangeTypeName } from '../types/api';
import { formatTimeWithDate } from '../utils/time';
import { SearchBar } from './shared';
import { SkeletonHistory } from './Skeleton/Skeleton';
import { useFocusTrap } from '../hooks/useFocusTrap';


interface RollbackConfirmation {
  operationId: string;
  containerName: string;
  oldVersion?: string;
  newVersion?: string;
  force?: boolean;
  operationType?: string; // 'label_change' for label rollbacks
  batchGroupId?: string;
}

interface BatchRollbackConfirmation {
  // Map of operationId -> container names for that operation
  selections: Map<string, { name: string; oldVersion: string; newVersion: string }[]>;
  isLabelChange?: boolean;
  batchGroupId?: string;
}

// A display item: either a single operation or a batch group
interface DisplayItem {
  type: 'single' | 'group';
  key: string; // operation_id or batch_group_id
  // For single operations
  operation?: UpdateOperation;
  // For batch groups
  groupId?: string;
  operations?: UpdateOperation[];
  allContainers?: BatchContainerDetail[];
  aggregateStatus?: string;
  earliestStart?: string;
  latestComplete?: string;
  containerCount?: number;
}

// Operation types that support rollback
const ROLLBACK_SUPPORTED_TYPES = ['single', 'batch', 'stack', 'label_change'];

// Rollback strategy per container
type RollbackStrategy = 'tag' | 'resolved' | 'digest' | 'none';

function getRollbackStrategy(detail: BatchContainerDetail): RollbackStrategy {
  if (detail.old_version !== detail.new_version) return 'tag';
  if (detail.old_resolved_version && detail.old_resolved_version !== detail.new_resolved_version) return 'resolved';
  if (detail.old_digest) return 'digest';
  return 'none';
}

function getRollbackStrategyNote(strategy: RollbackStrategy, detail: BatchContainerDetail): string | null {
  switch (strategy) {
    case 'resolved': return `Will pin to ${detail.old_resolved_version}`;
    case 'digest': return 'Will restore exact image by digest';
    case 'none': return detail.old_version === detail.new_version
      ? 'Same tag — no digest saved, cannot rollback'
      : 'Cannot rollback';
    default: return null;
  }
}

// Filter options for operation types
// Note: 'updates' is a UI filter that matches both 'single' and 'batch' operation types
type OperationType = 'all' | 'updates' | 'rollback' | 'restart' | 'label_change' | 'stop' | 'remove' | 'fix_mismatch';

export function History() {
  const navigate = useNavigate();
  const [operations, setOperations] = useState<UpdateOperation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<'all' | 'complete' | 'failed'>('all');
  const [typeFilter, setTypeFilter] = useState<OperationType>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [expandedOp, setExpandedOp] = useState<string | null>(null);
  const [rollbackConfirm, setRollbackConfirm] = useState<RollbackConfirmation | null>(null);
  const [batchRollbackConfirm, setBatchRollbackConfirm] = useState<BatchRollbackConfirmation | null>(null);
  const [selectedForRollback, setSelectedForRollback] = useState<Set<string>>(new Set());
  const cancelRollback = useCallback(() => {
    setRollbackConfirm(null);
    setBatchRollbackConfirm(null);
  }, []);

  // Focus trap for rollback confirmation dialog
  const dialogRef = useFocusTrap(!!rollbackConfirm || !!batchRollbackConfirm, cancelRollback);

  useEffect(() => {
    fetchOperations();
  }, []);

  const fetchOperations = async () => {
    setLoading(true);
    try {
      const response = await getOperations({ limit: 100 });
      if (response.success && response.data) {
        // Sort by completed_at or created_at DESC (most recent first)
        const sorted = response.data.operations.sort((a, b) => {
          const timeA = new Date(a.completed_at || a.created_at).getTime();
          const timeB = new Date(b.completed_at || b.created_at).getTime();
          return timeB - timeA;
        });
        setOperations(sorted);
      } else {
        setError(response.error || 'Failed to fetch operations');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  // Group operations by batch_group_id into display items
  const buildDisplayItems = (ops: UpdateOperation[]): DisplayItem[] => {
    const groupMap = new Map<string, UpdateOperation[]>();
    const singles: UpdateOperation[] = [];

    for (const op of ops) {
      if (op.batch_group_id) {
        if (!groupMap.has(op.batch_group_id)) {
          groupMap.set(op.batch_group_id, []);
        }
        groupMap.get(op.batch_group_id)!.push(op);
      } else {
        singles.push(op);
      }
    }

    const items: DisplayItem[] = [];

    // Add single operations
    for (const op of singles) {
      items.push({ type: 'single', key: op.operation_id, operation: op });
    }

    // Add batch groups
    for (const [groupId, groupOps] of groupMap) {
      if (groupOps.length === 1) {
        // Single operation in group — render as regular card
        items.push({ type: 'single', key: groupOps[0].operation_id, operation: groupOps[0] });
        continue;
      }

      // Merge all containers from all operations
      const allContainers: BatchContainerDetail[] = [];
      for (const op of groupOps) {
        if (op.batch_details && op.batch_details.length > 0) {
          allContainers.push(...op.batch_details);
        } else if (op.container_name) {
          allContainers.push({
            container_name: op.container_name,
            stack_name: op.stack_name,
            old_version: op.old_version || '',
            new_version: op.new_version || '',
          });
        }
      }

      // Aggregate status
      const allComplete = groupOps.every(op => op.status === 'complete');
      const anyFailed = groupOps.some(op => op.status === 'failed');
      const aggregateStatus = allComplete ? 'complete' : anyFailed ? 'failed' : 'partial';

      // Time range
      const starts = groupOps.filter(op => op.started_at).map(op => op.started_at!);
      const completes = groupOps.filter(op => op.completed_at).map(op => op.completed_at!);

      items.push({
        type: 'group',
        key: groupId,
        groupId,
        operations: groupOps,
        allContainers,
        aggregateStatus,
        earliestStart: starts.length > 0 ? starts.sort()[0] : groupOps[0].created_at,
        latestComplete: completes.length > 0 ? completes.sort().reverse()[0] : undefined,
        containerCount: allContainers.length,
      });
    }

    // Sort by time (most recent first)
    items.sort((a, b) => {
      const timeA = a.type === 'single'
        ? new Date(a.operation!.completed_at || a.operation!.created_at).getTime()
        : new Date(a.latestComplete || a.earliestStart || '0').getTime();
      const timeB = b.type === 'single'
        ? new Date(b.operation!.completed_at || b.operation!.created_at).getTime()
        : new Date(b.latestComplete || b.earliestStart || '0').getTime();
      return timeB - timeA;
    });

    return items;
  };

  const showRollbackConfirm = (op: UpdateOperation) => {
    setRollbackConfirm({
      operationId: op.operation_id,
      containerName: op.container_name,
      oldVersion: op.old_version,
      newVersion: op.new_version,
      operationType: op.operation_type,
      batchGroupId: op.batch_group_id,
    });
  };

  const executeRollback = (force = false) => {
    if (!rollbackConfirm) return;

    if (rollbackConfirm.operationType === 'label_change') {
      // Label rollback — navigate with labelRollback state
      navigate('/operation', {
        state: {
          labelRollback: {
            batchGroupId: rollbackConfirm.batchGroupId || rollbackConfirm.operationId,
            containers: [rollbackConfirm.containerName],
            containerNames: [rollbackConfirm.containerName],
          }
        }
      });
    } else {
      // Update rollback — navigate with rollback state
      navigate('/operation', {
        state: {
          rollback: {
            operationId: rollbackConfirm.operationId,
            containerName: rollbackConfirm.containerName,
            oldVersion: rollbackConfirm.oldVersion,
            newVersion: rollbackConfirm.newVersion,
            force,
          }
        }
      });
    }
    setRollbackConfirm(null);
  };

  const showBatchRollbackConfirm = (item: DisplayItem) => {
    if (!item.operations || !item.allContainers) return;

    // Build selections from checked containers
    const selections = new Map<string, { name: string; oldVersion: string; newVersion: string }[]>();

    for (const containerName of selectedForRollback) {
      // Find which operation this container belongs to
      for (const op of item.operations) {
        const detail = op.batch_details?.find(d => d.container_name === containerName);
        if (detail) {
          if (!selections.has(op.operation_id)) {
            selections.set(op.operation_id, []);
          }
          selections.get(op.operation_id)!.push({
            name: detail.container_name,
            oldVersion: detail.old_version,
            newVersion: detail.new_version,
          });
          break;
        }
        // Check if it's the top-level container
        if (op.container_name === containerName) {
          if (!selections.has(op.operation_id)) {
            selections.set(op.operation_id, []);
          }
          selections.get(op.operation_id)!.push({
            name: op.container_name,
            oldVersion: op.old_version || '',
            newVersion: op.new_version || '',
          });
          break;
        }
      }
    }

    if (selections.size === 0) return;
    const isLabelChange = item.operations?.every(op => op.operation_type === 'label_change') || false;
    setBatchRollbackConfirm({ selections, isLabelChange, batchGroupId: item.groupId });
  };

  const executeBatchRollback = async (force = false) => {
    if (!batchRollbackConfirm) return;

    // Check if these are label operations (all operations are label_change)
    const isLabelRollback = batchRollbackConfirm.isLabelChange;

    if (isLabelRollback && batchRollbackConfirm.batchGroupId) {
      // Label rollback — navigate to operation page with labelRollback state
      const allNames: string[] = [];
      for (const [, containers] of batchRollbackConfirm.selections) {
        allNames.push(...containers.map(c => c.name));
      }

      setBatchRollbackConfirm(null);
      setSelectedForRollback(new Set());

      navigate('/operation', {
        state: {
          labelRollback: {
            batchGroupId: batchRollbackConfirm.batchGroupId,
            containers: allNames,
            containerNames: allNames,
          }
        }
      });
      return;
    }

    const errors: string[] = [];
    const rollbackOpIds: string[] = [];
    const allNames: string[] = [];

    for (const [opId, containers] of batchRollbackConfirm.selections) {
      const names = containers.map(c => c.name);
      allNames.push(...names);
      try {
        const response = await rollbackContainers(opId, names, force);
        if (response.success && response.data?.operation_id) {
          rollbackOpIds.push(response.data.operation_id);
        } else {
          errors.push(response.error || `Failed to rollback ${names.join(', ')}`);
        }
      } catch (err) {
        errors.push(err instanceof Error ? err.message : `Failed to rollback ${names.join(', ')}`);
      }
    }

    setBatchRollbackConfirm(null);
    setSelectedForRollback(new Set());

    if (rollbackOpIds.length > 0) {
      // Navigate to recovery mode — rollback operations are already started by the API calls above.
      // Using URL params instead of state avoids the executor calling the API again.
      navigate(`/operation?id=${rollbackOpIds[0]}`);
    } else if (errors.length > 0) {
      setError(errors.join('; '));
    }
  };

  const toggleContainerRollback = (containerName: string) => {
    setSelectedForRollback(prev => {
      const next = new Set(prev);
      if (next.has(containerName)) {
        next.delete(containerName);
      } else {
        next.add(containerName);
      }
      return next;
    });
  };

  // Alias for consistency - using shared utility from utils/time.ts
  const formatTime = formatTimeWithDate;

  const formatDuration = (startedAt?: string, completedAt?: string, createdAt?: string) => {
    const start = startedAt ? new Date(startedAt).getTime() : (createdAt ? new Date(createdAt).getTime() : 0);
    const end = completedAt ? new Date(completedAt).getTime() : 0;
    if (!start || !end) return '-';

    const durationMs = end - start;
    if (durationMs < 1000) return `${durationMs}ms`;
    if (durationMs < 60000) return `${(durationMs / 1000).toFixed(0)}s`;
    return `${(durationMs / 60000).toFixed(1)}m`;
  };

  const getStatusIcon = (status: string, rollback: boolean) => {
    if (rollback) return <i className="fa-solid fa-rotate-left"></i>;
    switch (status) {
      case 'complete': return <i className="fa-solid fa-check"></i>;
      case 'failed': return <i className="fa-solid fa-xmark"></i>;
      case 'partial': return <i className="fa-solid fa-exclamation-triangle"></i>;
      default: return <i className="fa-regular fa-circle"></i>;
    }
  };

  const getStatusClass = (status: string, rollback: boolean = false) => {
    if (rollback) return 'status-rollback';
    switch (status) {
      case 'complete': return 'status-success';
      case 'failed': return 'status-failed';
      case 'partial': return 'status-failed';
      default: return 'status-pending';
    }
  };

  const filteredOperations = operations.filter(op => {
    // Status filter
    if (statusFilter === 'complete' && op.status !== 'complete') return false;
    if (statusFilter === 'failed' && !(op.status === 'failed' || op.rollback_occurred)) return false;

    // Type filter
    if (typeFilter !== 'all') {
      if (typeFilter === 'updates') {
        // 'updates' filter shows both single and batch update operations
        if (op.operation_type !== 'single' && op.operation_type !== 'batch' && op.operation_type !== 'stack') return false;
      } else if (op.operation_type !== typeFilter) {
        return false;
      }
    }

    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      const matchesContainer = op.container_name?.toLowerCase().includes(query);
      const matchesStack = op.stack_name?.toLowerCase().includes(query);
      const matchesId = op.operation_id.toLowerCase().includes(query);
      const matchesOldVersion = op.old_version?.toLowerCase().includes(query);
      const matchesNewVersion = op.new_version?.toLowerCase().includes(query);
      const matchesType = op.operation_type?.toLowerCase().includes(query);
      const matchesBatchDetail = op.batch_details?.some(d =>
        d.container_name.toLowerCase().includes(query)
      );

      if (!matchesContainer && !matchesStack && !matchesId && !matchesOldVersion && !matchesNewVersion && !matchesType && !matchesBatchDetail) {
        return false;
      }
    }

    return true;
  });

  const displayItems = buildDisplayItems(filteredOperations);

  // Check if an operation type supports rollback
  const canRollback = (op: UpdateOperation): boolean => {
    return ROLLBACK_SUPPORTED_TYPES.includes(op.operation_type) &&
           (op.status === 'complete' || op.status === 'failed') &&
           !op.rollback_occurred;
  };

  // Check if a batch group supports rollback
  const canGroupRollback = (item: DisplayItem): boolean => {
    if (item.type !== 'group' || !item.operations) return false;
    return item.operations.some(op => canRollback(op));
  };

  // Format version display with resolved version in parentheses
  const formatVersionDisplay = (tag: string, resolvedVersion?: string): string => {
    if (!tag) return resolvedVersion || '';
    if (!resolvedVersion || resolvedVersion === tag || tag.includes(resolvedVersion)) {
      return tag;
    }
    return `${tag} (${resolvedVersion})`;
  };

  // Render a change type badge for a batch detail
  const renderChangeTypeBadge = (detail: BatchContainerDetail) => {
    const ct = detail.change_type;
    if (ct === undefined || ct === null) return null;
    const name = getChangeTypeName(ct);
    const label = ct === ChangeType.NoChange ? 'REBUILD'
      : ct === ChangeType.PatchChange ? 'PATCH'
      : ct === ChangeType.MinorChange ? 'MINOR'
      : ct === ChangeType.MajorChange ? 'MAJOR'
      : ct === ChangeType.Downgrade ? 'DOWNGRADE'
      : null;
    if (!label) return null;
    return <span className={`op-type-badge ${name}`}>{label}</span>;
  };

  const renderSingleCard = (op: UpdateOperation) => (
    <div
      key={op.operation_id}
      className={`operation-card ${getStatusClass(op.status, op.rollback_occurred)} ${expandedOp === op.operation_id ? 'expanded' : ''}`}
    >
      <div className="operation-summary" onClick={() => {
        const wasExpanded = expandedOp === op.operation_id;
        setExpandedOp(wasExpanded ? null : op.operation_id);
        setSelectedForRollback(new Set());
      }}>
        <div className="op-main">
          <span className={`op-status-icon ${getStatusClass(op.status, op.rollback_occurred)}`}>
            {getStatusIcon(op.status, op.rollback_occurred)}
          </span>
          <span className="op-container">
            {op.operation_type === 'batch' ? (
              <>{op.stack_name || 'Batch'} <span className="op-type-badge batch">BATCH</span></>
            ) : (
              <span
                className="container-link"
                onClick={(e) => {
                  e.stopPropagation();
                  if (op.container_name) {
                    navigate(`/container/${op.container_name}`);
                  }
                }}
              >
                {op.container_name || op.stack_name || 'Unknown'}
              </span>
            )}
          </span>
          {op.operation_type === 'rollback' && (
            <span className="op-type-badge rollback">ROLLBACK</span>
          )}
          {op.operation_type === 'restart' && (
            <span className="op-type-badge restart">RESTART</span>
          )}
          {op.operation_type === 'label_change' && (
            <span className="op-type-badge labels">LABELS</span>
          )}
          {op.operation_type === 'stop' && (
            <span className="op-type-badge stop">STOP</span>
          )}
          {op.operation_type === 'remove' && (
            <span className="op-type-badge remove">REMOVE</span>
          )}
          {op.operation_type === 'fix_mismatch' && (
            <span className="op-type-badge fix">FIX</span>
          )}
          {/* Change type badge for single update operations */}
          {(op.operation_type === 'single' || op.operation_type === 'stack') && op.batch_details?.[0] && renderChangeTypeBadge(op.batch_details[0])}
          {op.rollback_occurred && (
            <span className="op-type-badge rolled-back">ROLLED BACK</span>
          )}
        </div>
        <div className="op-info">
          {op.operation_type === 'batch' && (
            <span className="op-batch-summary">
              {op.batch_details && op.batch_details.length > 0
                ? (() => {
                    const names = op.batch_details.map(d => d.container_name);
                    const maxShow = 3;
                    if (names.length <= maxShow) return names.join(', ');
                    return `${names.slice(0, maxShow).join(', ')} +${names.length - maxShow} more`;
                  })()
                : op.error_message?.replace('Batch update completed: ', '') || ''}
            </span>
          )}
          {op.operation_type === 'label_change' && (
            <span className="op-label-info">Label configuration changed</span>
          )}
          {op.operation_type === 'stop' && (
            <span className="op-label-info">Container stopped</span>
          )}
          {op.operation_type === 'remove' && (
            <span className="op-label-info">Container removed</span>
          )}
          {op.operation_type === 'fix_mismatch' && (
            <span className="op-label-info">Compose mismatch fixed</span>
          )}
          {op.operation_type !== 'batch' && op.operation_type !== 'label_change' && op.operation_type !== 'restart' && op.operation_type !== 'stop' && op.operation_type !== 'remove' && op.operation_type !== 'fix_mismatch' && op.new_version && (
            <span className="op-version">
              {(() => {
                const detail = op.batch_details?.[0];
                const oldDisplay = detail ? formatVersionDisplay(op.old_version || '', detail.old_resolved_version) : (op.old_version || '');
                const newDisplay = detail ? formatVersionDisplay(op.new_version, detail.new_resolved_version) : op.new_version;
                return oldDisplay && oldDisplay !== newDisplay ? (
                  <>{oldDisplay} → {newDisplay}</>
                ) : (
                  <>{newDisplay}</>
                );
              })()}
            </span>
          )}
        </div>
        <div className="op-meta">
          <button
            className="op-copy-btn"
            title={`Copy ID: ${op.operation_id}`}
            onClick={(e) => {
              e.stopPropagation();
              navigator.clipboard.writeText(op.operation_id);
              const btn = e.currentTarget;
              btn.classList.add('copied');
              setTimeout(() => btn.classList.remove('copied'), 1500);
            }}
          >
            <i className="fa-regular fa-copy"></i>
            <span className="op-id-short">{op.operation_id.slice(0, 8)}</span>
          </button>
          <span className="op-time">{formatTime(op.completed_at || op.created_at)}</span>
          <span className="op-duration">{formatDuration(op.started_at, op.completed_at, op.created_at)}</span>
        </div>
      </div>

      <div className="operation-details-wrapper" onClick={(e) => e.stopPropagation()}>
        <div className="operation-expanded">
          <div className="op-detail-grid">
            <div className="op-detail">
              <span className="label">Stack</span>
              <span className="value">{op.stack_name || 'standalone'}</span>
            </div>
            <div className="op-detail">
              <span className="label">Type</span>
              <span className="value">{op.operation_type}</span>
            </div>
            <div className="op-detail">
              <span className="label">Status</span>
              <span className={`value ${getStatusClass(op.status)}`}>{op.status}</span>
            </div>
            {op.completed_at && (
              <div className="op-detail">
                <span className="label">Completed</span>
                <span className="value">{new Date(op.completed_at).toLocaleString()}</span>
              </div>
            )}
          </div>

          {/* Batch Details - show individual containers and version transitions */}
          {op.batch_details && op.batch_details.length > 0 && (
            <div className="op-batch-details">
              <div className="batch-details-header">
                Containers ({op.batch_details.length})
                {op.batch_details.length > 1 && canRollback(op) && (
                  <span className="batch-rollback-hint">Select containers to rollback</span>
                )}
              </div>
              <div className="batch-details-list">
                {op.batch_details.map((detail, idx) => {
                  const hasRollback = op.batch_details!.length > 1 && canRollback(op);
                  const strategy = getRollbackStrategy(detail);
                  const strategyNote = getRollbackStrategyNote(strategy, detail);
                  const isNonRollbackable = strategy === 'none';
                  return (
                    <div
                      key={idx}
                      className={`batch-detail-item ${hasRollback && !isNonRollbackable ? 'selectable' : ''} ${isNonRollbackable ? 'non-rollbackable' : ''}`}
                      onClick={hasRollback && !isNonRollbackable ? () => toggleContainerRollback(detail.container_name) : undefined}
                    >
                      {hasRollback && (
                        <input
                          type="checkbox"
                          className="batch-rollback-checkbox"
                          checked={selectedForRollback.has(detail.container_name)}
                          disabled={isNonRollbackable}
                          readOnly
                        />
                      )}
                      <div className="batch-detail-content">
                        <div className="batch-detail-row">
                          <span className="batch-container-name">
                            {detail.container_name}
                          </span>
                          <span className="batch-version-change">
                            {formatVersionDisplay(detail.old_version, detail.old_resolved_version)} → {formatVersionDisplay(detail.new_version, detail.new_resolved_version)}
                          </span>
                          {renderChangeTypeBadge(detail)}
                        </div>
                        {strategyNote && (
                          <div className={`rollback-strategy-note ${strategy}`}>
                            <i className={`fa-solid ${isNonRollbackable ? 'fa-triangle-exclamation' : 'fa-circle-info'}`}></i>
                            {strategyNote}
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {op.error_message && op.status === 'failed' && (
            <div className="op-error">
              <span className="error-label">Error:</span>
              <span className="error-msg">{op.error_message}</span>
            </div>
          )}

          <div className="op-actions">
            {/* Per-container rollback for batch operations with selections */}
            {op.batch_details && op.batch_details.length > 1 && canRollback(op) && selectedForRollback.size > 0 && expandedOp === op.operation_id && (
              <button
                className="rollback-btn"
                onClick={(e) => {
                  e.stopPropagation();
                  // Build selections map for batch rollback
                  const selections = new Map<string, { name: string; oldVersion: string; newVersion: string }[]>();
                  const containers: { name: string; oldVersion: string; newVersion: string }[] = [];
                  for (const containerName of selectedForRollback) {
                    const detail = op.batch_details?.find(d => d.container_name === containerName);
                    if (detail) {
                      containers.push({
                        name: detail.container_name,
                        oldVersion: detail.old_version,
                        newVersion: detail.new_version,
                      });
                    }
                  }
                  if (containers.length > 0) {
                    selections.set(op.operation_id, containers);
                    setBatchRollbackConfirm({ selections });
                  }
                }}
              >
                <i className="fa-solid fa-rotate-left"></i> Rollback Selected ({selectedForRollback.size})
              </button>
            )}
            {/* Full rollback for single-container ops or when no per-container selection */}
            {canRollback(op) && !(op.batch_details && op.batch_details.length > 1 && selectedForRollback.size > 0 && expandedOp === op.operation_id) && (
              <button
                className="rollback-btn"
                onClick={(e) => {
                  e.stopPropagation();
                  if (op.batch_details && op.batch_details.length > 1) {
                    // Batch operation — use batch rollback flow so recovery creates per-container entries
                    const selections = new Map<string, Array<{ name: string; oldVersion: string; newVersion: string }>>();
                    const containers = op.batch_details
                      .filter(d => getRollbackStrategy(d) !== 'none')
                      .map(d => ({ name: d.container_name, oldVersion: d.old_version, newVersion: d.new_version }));
                    if (containers.length > 0) {
                      selections.set(op.operation_id, containers);
                      const isLabelChange = op.operation_type === 'label_change';
                      setBatchRollbackConfirm({ selections, isLabelChange });
                    }
                  } else {
                    showRollbackConfirm(op);
                  }
                }}
              >
                <i className="fa-solid fa-rotate-left"></i> Rollback{op.batch_details && op.batch_details.length > 1 ? ' All' : ''}
              </button>
            )}
            <span className="op-id">ID: {op.operation_id.slice(0, 12)}</span>
          </div>
        </div>
      </div>
    </div>
  );

  const renderGroupCard = (item: DisplayItem) => {
    const isExpanded = expandedOp === item.key;
    const anyRolledBack = item.operations?.some(op => op.rollback_occurred) || false;
    const hasSelections = selectedForRollback.size > 0 && isExpanded;

    return (
      <div
        key={item.key}
        className={`operation-card ${getStatusClass(item.aggregateStatus || 'complete')} ${isExpanded ? 'expanded' : ''}`}
      >
        <div className="operation-summary" onClick={() => {
          setExpandedOp(isExpanded ? null : item.key);
          setSelectedForRollback(new Set());
        }}>
          <div className="op-main">
            <span className={`op-status-icon ${getStatusClass(item.aggregateStatus || 'complete')}`}>
              {getStatusIcon(item.aggregateStatus || 'complete', anyRolledBack)}
            </span>
            <span className="op-container">
              {(() => {
                const opType = item.operations?.[0]?.operation_type;
                if (opType === 'label_change') return <>Label Change <span className="op-type-badge label">LABELS</span></>;
                if (opType === 'stop') return <>Batch Stop <span className="op-type-badge stop">STOP</span></>;
                if (opType === 'start') return <>Batch Start <span className="op-type-badge restart">START</span></>;
                if (opType === 'restart') return <>Batch Restart <span className="op-type-badge restart">RESTART</span></>;
                if (opType === 'remove') return <>Batch Remove <span className="op-type-badge remove">REMOVE</span></>;
                return <>Batch Update <span className="op-type-badge batch">BATCH</span></>;
              })()}
            </span>
            <span className="op-container-count">{item.containerCount} containers</span>
            {anyRolledBack && (
              <span className="op-type-badge rolled-back">ROLLED BACK</span>
            )}
          </div>
          <div className="op-info">
            {item.aggregateStatus === 'partial' ? (
              <span className="op-batch-summary">Some operations failed</span>
            ) : item.allContainers && item.allContainers.length > 0 && (
              <span className="op-batch-summary">
                {(() => {
                  const names = item.allContainers!.map(c => c.container_name);
                  const maxShow = 3;
                  if (names.length <= maxShow) return names.join(', ');
                  return `${names.slice(0, maxShow).join(', ')} +${names.length - maxShow} more`;
                })()}
              </span>
            )}
          </div>
          <div className="op-meta">
            <span className="op-time">{formatTime(item.latestComplete || item.earliestStart || '')}</span>
            <span className="op-duration">{formatDuration(item.earliestStart, item.latestComplete)}</span>
          </div>
        </div>

        <div className="operation-details-wrapper" onClick={(e) => e.stopPropagation()}>
          <div className="operation-expanded">
            <div className="op-detail-grid">
              <div className="op-detail">
                <span className="label">Operations</span>
                <span className="value">{item.operations?.length || 0}</span>
              </div>
              <div className="op-detail">
                <span className="label">Containers</span>
                <span className="value">{item.containerCount}</span>
              </div>
              <div className="op-detail">
                <span className="label">Status</span>
                <span className={`value ${getStatusClass(item.aggregateStatus || 'complete')}`}>
                  {item.aggregateStatus === 'complete' ? 'complete' : item.aggregateStatus === 'failed' ? 'failed' : 'partial'}
                </span>
              </div>
            </div>

            {/* Per-container list with rollback checkboxes */}
            <div className="op-batch-details">
              <div className="batch-details-header">
                Containers ({item.containerCount})
                {canGroupRollback(item) && (
                  <span className="batch-rollback-hint">Select containers to rollback</span>
                )}
              </div>
              <div className="batch-details-list">
                {item.allContainers?.map((detail, idx) => {
                  // Find the operation this container belongs to
                  const ownerOp = item.operations?.find(op =>
                    op.batch_details?.some(d => d.container_name === detail.container_name) ||
                    op.container_name === detail.container_name
                  );
                  const opCanRollback = ownerOp ? canRollback(ownerOp) : false;
                  const strategy = getRollbackStrategy(detail);
                  const strategyNote = getRollbackStrategyNote(strategy, detail);
                  const isNonRollbackable = strategy === 'none';
                  const isSelectable = canGroupRollback(item) && opCanRollback && !isNonRollbackable;

                  return (
                    <div
                      key={idx}
                      className={`batch-detail-item ${isSelectable ? 'selectable' : ''} ${isNonRollbackable ? 'non-rollbackable' : ''}`}
                      onClick={isSelectable ? () => toggleContainerRollback(detail.container_name) : undefined}
                    >
                      {canGroupRollback(item) && (
                        <input
                          type="checkbox"
                          className="batch-rollback-checkbox"
                          checked={selectedForRollback.has(detail.container_name)}
                          disabled={!opCanRollback || isNonRollbackable}
                          readOnly
                        />
                      )}
                      <div className="batch-detail-content">
                        <div className="batch-detail-row">
                          <span className="batch-container-name">
                            {detail.container_name}
                          </span>
                          {detail.old_version || detail.new_version ? (
                            <>
                              <span className="batch-version-change">
                                {formatVersionDisplay(detail.old_version, detail.old_resolved_version)} → {formatVersionDisplay(detail.new_version, detail.new_resolved_version)}
                              </span>
                              {renderChangeTypeBadge(detail)}
                            </>
                          ) : (
                            <span className={`batch-op-status ${ownerOp?.status || 'complete'}`}>
                              {ownerOp?.status === 'failed' ? ownerOp.error_message || 'Failed' : ownerOp?.status === 'complete' ? 'Done' : ownerOp?.status || ''}
                            </span>
                          )}
                        </div>
                        {strategyNote && (
                          <div className={`rollback-strategy-note ${strategy}`}>
                            <i className={`fa-solid ${isNonRollbackable ? 'fa-triangle-exclamation' : 'fa-circle-info'}`}></i>
                            {strategyNote}
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Show errors from any failed operations */}
            {item.operations?.filter(op => op.status === 'failed' && op.error_message).map(op => (
              <div key={op.operation_id} className="op-error">
                <span className="error-label">Error ({op.container_name}):</span>
                <span className="error-msg">{op.error_message}</span>
              </div>
            ))}

            <div className="op-actions">
              {hasSelections && (
                <button
                  className="rollback-btn"
                  onClick={(e) => {
                    e.stopPropagation();
                    showBatchRollbackConfirm(item);
                  }}
                >
                  <i className="fa-solid fa-rotate-left"></i> Rollback Selected ({selectedForRollback.size})
                </button>
              )}
              {!hasSelections && canGroupRollback(item) && (
                <button
                  className="rollback-btn"
                  onClick={(e) => {
                    e.stopPropagation();
                    // Select all rollbackable containers (exclude 'none' strategy)
                    const allNames = new Set<string>();
                    for (const detail of item.allContainers || []) {
                      if (getRollbackStrategy(detail) === 'none') continue;
                      const ownerOp = item.operations?.find(op =>
                        op.batch_details?.some(d => d.container_name === detail.container_name) ||
                        op.container_name === detail.container_name
                      );
                      if (ownerOp && canRollback(ownerOp)) {
                        allNames.add(detail.container_name);
                      }
                    }
                    setSelectedForRollback(allNames);
                    // Build confirmation directly
                    const selections = new Map<string, { name: string; oldVersion: string; newVersion: string }[]>();
                    for (const containerName of allNames) {
                      for (const op of item.operations || []) {
                        const detail = op.batch_details?.find(d => d.container_name === containerName);
                        if (detail) {
                          if (!selections.has(op.operation_id)) selections.set(op.operation_id, []);
                          selections.get(op.operation_id)!.push({ name: detail.container_name, oldVersion: detail.old_version, newVersion: detail.new_version });
                          break;
                        }
                        if (op.container_name === containerName) {
                          if (!selections.has(op.operation_id)) selections.set(op.operation_id, []);
                          selections.get(op.operation_id)!.push({ name: op.container_name, oldVersion: op.old_version || '', newVersion: op.new_version || '' });
                          break;
                        }
                      }
                    }
                    const isLabelChange = item.operations?.every(op => op.operation_type === 'label_change') || false;
                    setBatchRollbackConfirm({ selections, isLabelChange, batchGroupId: item.groupId });
                  }}
                >
                  <i className="fa-solid fa-rotate-left"></i> Rollback All
                </button>
              )}
              <span className="op-id">Group: {item.groupId?.slice(0, 12)}</span>
            </div>
          </div>
        </div>
      </div>
    );
  };

  if (loading) {
    return (
      <div className="history-page">
        <header>
          <div className="header-top">
            <h1>History</h1>
          </div>
          <SearchBar
            value=""
            onChange={() => {}}
            placeholder="Search operations..."
            disabled
            className="search-bar-loading"
          />
          <div className="filter-toolbar">
            <div className="segmented-control">
              <button disabled className="active">All</button>
              <button disabled>Success</button>
              <button disabled>Failed</button>
            </div>
            <select disabled className="type-filter-select">
              <option>All Types</option>
            </select>
          </div>
        </header>
        <main className="history-list">
          <SkeletonHistory count={5} />
        </main>
      </div>
    );
  }

  if (error) {
    return (
      <div className="history-page">
        <header>
          <div className="header-top">
          </div>
        </header>
        <div className="error">
          <p>{error}</p>
          <button onClick={fetchOperations}>Retry</button>
        </div>
      </div>
    );
  }

  return (
    <div className="history-page">
      <header>
        <div className="header-top">
          <h1>History</h1>
        </div>
        <SearchBar
          value={searchQuery}
          onChange={setSearchQuery}
          placeholder="Search operations..."
        />
        <div className="filter-toolbar">
          <div className="segmented-control">
            <button
              className={statusFilter === 'all' ? 'active' : ''}
              onClick={() => setStatusFilter('all')}
            >
              All
            </button>
            <button
              className={statusFilter === 'complete' ? 'active' : ''}
              onClick={() => setStatusFilter('complete')}
            >
              Success
            </button>
            <button
              className={statusFilter === 'failed' ? 'active' : ''}
              onClick={() => setStatusFilter('failed')}
            >
              Failed
            </button>
          </div>
          <select
            className="type-filter-select"
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value as OperationType)}
            aria-label="Filter by operation type"
          >
            <option value="all">All Types</option>
            <option value="updates">Updates</option>
            <option value="rollback">Rollbacks</option>
            <option value="restart">Restarts</option>
            <option value="stop">Stops</option>
            <option value="remove">Removals</option>
            <option value="label_change">Labels</option>
            <option value="fix_mismatch">Fix Mismatch</option>
          </select>
        </div>
      </header>

      <main className="history-list">
        {displayItems.length === 0 ? (
          <div className="empty">No operations found</div>
        ) : (
          displayItems.map(item =>
            item.type === 'single'
              ? renderSingleCard(item.operation!)
              : renderGroupCard(item)
          )
        )}
      </main>

      {/* Single Operation Rollback Confirmation Dialog */}
      {rollbackConfirm && (
        <div className="confirm-dialog-overlay">
          <div
            className="confirm-dialog"
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="rollback-dialog-title"
          >
            <div className="confirm-dialog-header">
              <h3 id="rollback-dialog-title">Confirm Rollback</h3>
            </div>
            <div className="confirm-dialog-body">
              {rollbackConfirm.operationType === 'label_change' ? (
                <>
                  <p>Restore previous labels for <strong>{rollbackConfirm.containerName}</strong>?</p>
                  <p className="confirm-warning">This will reverse the label changes and restart the container.</p>
                </>
              ) : (
                <>
                  <p>Roll back <strong>{rollbackConfirm.containerName}</strong> to its previous version?</p>
                  {rollbackConfirm.newVersion && rollbackConfirm.oldVersion && (
                    <div className="confirm-version-change">
                      <span className="version-current">{rollbackConfirm.newVersion}</span>
                      <span className="version-arrow">→</span>
                      <span className="version-target">{rollbackConfirm.oldVersion}</span>
                    </div>
                  )}
                  <p className="confirm-warning">This will recreate the container with the previous image.</p>
                </>
              )}
            </div>
            <div className="confirm-dialog-actions">
              <button className="confirm-cancel" onClick={cancelRollback}>Cancel</button>
              <button className="confirm-proceed" onClick={() => executeRollback(false)}>Rollback</button>
              <button className="confirm-force" onClick={() => executeRollback(true)} title="Skip pre-update checks on dependent containers">
                <i className="fa-solid fa-bolt"></i> Force
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Batch Rollback Confirmation Dialog */}
      {batchRollbackConfirm && (
        <div className="confirm-dialog-overlay">
          <div
            className="confirm-dialog"
            ref={dialogRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby="batch-rollback-dialog-title"
          >
            <div className="confirm-dialog-header">
              <h3 id="batch-rollback-dialog-title">Confirm Per-Container Rollback</h3>
            </div>
            <div className="confirm-dialog-body">
              <p>Roll back the following containers to their previous versions?</p>
              <div className="confirm-container-list">
                {Array.from(batchRollbackConfirm.selections.values()).flat().map((c, idx) => (
                  <div key={idx} className="confirm-container-item">
                    <strong>{c.name}</strong>
                    {c.newVersion && c.oldVersion && (
                      <span className="confirm-version-change">
                        <span className="version-current">{c.newVersion}</span>
                        <span className="version-arrow">→</span>
                        <span className="version-target">{c.oldVersion}</span>
                      </span>
                    )}
                  </div>
                ))}
              </div>
              <p className="confirm-warning">This will recreate selected containers with their previous images.</p>
            </div>
            <div className="confirm-dialog-actions">
              <button className="confirm-cancel" onClick={cancelRollback}>Cancel</button>
              <button className="confirm-proceed" onClick={() => executeBatchRollback(false)}>Rollback</button>
              <button className="confirm-force" onClick={() => executeBatchRollback(true)} title="Skip pre-update checks on dependent containers">
                <i className="fa-solid fa-bolt"></i> Force
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
