import { useState, useEffect } from 'react';
import { getScripts, getScriptAssignments, assignScript, unassignScript } from '../api/client';
import type { Script, ScriptAssignment, ContainerInfo } from '../types/api';

interface ScriptManagerProps {
  containers: ContainerInfo[];
  onClose: () => void;
}

export function ScriptManager({ containers, onClose }: ScriptManagerProps) {
  const [scripts, setScripts] = useState<Script[]>([]);
  const [assignments, setAssignments] = useState<ScriptAssignment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedContainer, setSelectedContainer] = useState<string | null>(null);
  const [selectedScript, setSelectedScript] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [activeTab, setActiveTab] = useState<'assign' | 'list'>('assign');

  useEffect(() => {
    fetchData();
  }, []);

  const fetchData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [scriptsResponse, assignmentsResponse] = await Promise.all([
        getScripts(),
        getScriptAssignments(),
      ]);

      if (scriptsResponse.success && scriptsResponse.data) {
        setScripts(scriptsResponse.data.scripts || []);
      } else {
        setError(scriptsResponse.error || 'Failed to fetch scripts');
      }

      if (assignmentsResponse.success && assignmentsResponse.data) {
        setAssignments(assignmentsResponse.data.assignments || []);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const handleAssign = async () => {
    if (!selectedContainer || !selectedScript) return;

    setSaving(true);
    try {
      const response = await assignScript(selectedContainer, selectedScript);
      if (response.success) {
        await fetchData();
        setSelectedContainer(null);
        setSelectedScript(null);
      } else {
        setError(response.error || 'Failed to assign script');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setSaving(false);
    }
  };

  const handleUnassign = async (containerName: string) => {
    if (!confirm(`Remove script assignment from ${containerName}?`)) return;

    setSaving(true);
    try {
      const response = await unassignScript(containerName);
      if (response.success) {
        await fetchData();
      } else {
        setError(response.error || 'Failed to unassign script');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setSaving(false);
    }
  };

  const getAssignmentForContainer = (containerName: string): ScriptAssignment | undefined => {
    return assignments.find(a => a.container_name === containerName);
  };

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return dateStr;
    }
  };

  const formatFileSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  if (loading) {
    return (
      <div className="modal-overlay">
        <div className="modal script-manager-modal">
          <div className="modal-header">
            <h2>Script Manager</h2>
            <button className="close-btn" onClick={onClose}>×</button>
          </div>
          <div className="modal-body">
            <div className="loading-content">
              <div className="spinner"></div>
              <p>Loading scripts...</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="modal-overlay">
      <div className="modal script-manager-modal">
        <div className="modal-header">
          <h2>Script Manager</h2>
          <button className="close-btn" onClick={onClose}>×</button>
        </div>

        {error && (
          <div className="error-banner">
            <i className="fa-solid fa-triangle-exclamation"></i>
            {error}
            <button onClick={() => setError(null)}>×</button>
          </div>
        )}

        <div className="tabs-underline modal-tabs">
          <button
            className={activeTab === 'assign' ? 'active' : ''}
            onClick={() => setActiveTab('assign')}
          >
            Assign Scripts
          </button>
          <button
            className={activeTab === 'list' ? 'active' : ''}
            onClick={() => setActiveTab('list')}
          >
            Available Scripts ({scripts.length})
          </button>
        </div>

        <div className="modal-body">
          {activeTab === 'assign' && (
            <div className="assign-scripts-section">
              <div className="assign-form">
                <div className="form-group">
                  <label>Container</label>
                  <select
                    value={selectedContainer || ''}
                    onChange={(e) => setSelectedContainer(e.target.value)}
                    disabled={saving}
                  >
                    <option value="">Select container...</option>
                    {containers.map(c => (
                      <option key={c.container_name} value={c.container_name}>
                        {c.container_name}
                        {getAssignmentForContainer(c.container_name) ? ' (assigned)' : ''}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="form-group">
                  <label>Script</label>
                  <select
                    value={selectedScript || ''}
                    onChange={(e) => setSelectedScript(e.target.value)}
                    disabled={saving || !selectedContainer}
                  >
                    <option value="">Select script...</option>
                    {scripts.map(s => (
                      <option key={s.path} value={s.relative_path}>
                        {s.name} {s.executable ? '✓' : '✗'}
                      </option>
                    ))}
                  </select>
                </div>

                <button
                  className="button button-primary button-inline"
                  onClick={handleAssign}
                  disabled={!selectedContainer || !selectedScript || saving}
                >
                  {saving ? 'Assigning...' : 'Assign Script'}
                </button>
              </div>

              <div className="assignments-list">
                <h3>Current Assignments</h3>
                {assignments.length === 0 ? (
                  <p className="empty-state">No script assignments yet</p>
                ) : (
                  <ul>
                    {assignments.map(assignment => (
                      <li key={assignment.container_name} className="assignment-item">
                        <div className="assignment-info">
                          <div className="assignment-container">
                            <i className="fa-solid fa-cube"></i>
                            {assignment.container_name}
                          </div>
                          <div className="assignment-script">
                            <i className="fa-solid fa-file-code"></i>
                            {assignment.script_path}
                          </div>
                          <div className="assignment-meta">
                            Assigned {formatDate(assignment.assigned_at)}
                            {assignment.assigned_by && ` by ${assignment.assigned_by}`}
                          </div>
                        </div>
                        <button
                          className="button button-ghost button-danger button-icon button-sm"
                          onClick={() => handleUnassign(assignment.container_name)}
                          disabled={saving}
                        >
                          <i className="fa-solid fa-trash"></i>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>

              <div className="info-box">
                <i className="fa-solid fa-info-circle"></i>
                <div>
                  <strong>Important:</strong> After assigning or unassigning a script, you must restart the container for the changes to take effect.
                </div>
              </div>
            </div>
          )}

          {activeTab === 'list' && (
            <div className="scripts-list-section">
              {scripts.length === 0 ? (
                <p className="empty-state">No scripts found in /scripts folder</p>
              ) : (
                <ul className="scripts-list">
                  {scripts.map(script => (
                    <li key={script.path} className="script-item">
                      <div className="script-icon">
                        {script.executable ? (
                          <i className="fa-solid fa-file-circle-check"></i>
                        ) : (
                          <i className="fa-solid fa-file-circle-xmark"></i>
                        )}
                      </div>
                      <div className="script-info">
                        <div className="script-name">{script.name}</div>
                        <div className="script-meta">
                          <span>{formatFileSize(script.size)}</span>
                          <span>•</span>
                          <span>Modified {formatDate(script.modified_time)}</span>
                          <span>•</span>
                          <span className={script.executable ? 'executable-yes' : 'executable-no'}>
                            {script.executable ? 'Executable ✓' : 'Not executable ✗'}
                          </span>
                        </div>
                        <div className="script-path">{script.relative_path}</div>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>

        <div className="modal-footer">
          <button className="button button-secondary button-inline" onClick={onClose}>Close</button>
        </div>
      </div>
    </div>
  );
}
