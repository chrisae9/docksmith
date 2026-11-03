import { useState, useEffect, useRef } from 'react';
import {
  getNotifySettings,
  updateNotifySettings,
  validateNotifyCron,
  listNotifyServices,
  createNotifyService,
  updateNotifyService,
  deleteNotifyService,
  testNotifyService,
} from '../api/client';
import type {
  EventKind,
  NotifySettings,
  NotifySettingsPayload,
  NotifyService,
  NotifyServiceInput,
  NotifyTiming,
} from '../types/api';
import { formatTimeAgo } from '../utils/time';

const MASKED_SECRET = '********';
const CRON_DEBOUNCE_MS = 400;

const EVENT_KINDS: { kind: EventKind; label: string }[] = [
  { kind: 'new_version', label: 'New version available' },
  { kind: 'digest_changed', label: 'Digest changed' },
  { kind: 'update_succeeded', label: 'Update succeeded' },
  { kind: 'update_failed', label: 'Update failed' },
  { kind: 'rollback', label: 'Rollback performed' },
  { kind: 'update_blocked', label: 'Update blocked by pre-update check' },
  { kind: 'check_failing', label: 'Version check failing' },
  { kind: 'compose_mismatch', label: 'Compose mismatch' },
];

const MIN_LEVELS = ['major', 'minor', 'patch', 'build'];

interface FieldDef {
  key: string;
  label: string;
  secret?: boolean;
  required?: boolean;
  placeholder?: string;
  options?: string[];
}

// Config keys per service type; mirrors internal/notify/services.go.
const SERVICE_TYPES: { type: string; label: string; fields: FieldDef[] }[] = [
  {
    type: 'smtp', label: 'Email (SMTP)', fields: [
      { key: 'host', label: 'Host', required: true },
      { key: 'port', label: 'Port', placeholder: '587' },
      { key: 'username', label: 'Username' },
      { key: 'password', label: 'Password', secret: true },
      { key: 'encryption', label: 'Encryption', options: ['', 'none', 'starttls', 'tls'] },
      { key: 'from', label: 'From', required: true, placeholder: 'Docksmith <docksmith@example.com>' },
      { key: 'to', label: 'To', required: true, placeholder: 'you@example.com, Name <other@example.com>' },
    ],
  },
  {
    type: 'ntfy', label: 'ntfy', fields: [
      { key: 'host', label: 'Host', placeholder: 'ntfy.sh' },
      { key: 'topic', label: 'Topic', required: true },
      { key: 'token', label: 'Access token', secret: true },
      { key: 'username', label: 'Username' },
      { key: 'password', label: 'Password', secret: true },
    ],
  },
  { type: 'discord', label: 'Discord', fields: [{ key: 'webhook_url', label: 'Webhook URL', secret: true, required: true }] },
  {
    type: 'telegram', label: 'Telegram', fields: [
      { key: 'token', label: 'Bot token', secret: true, required: true, placeholder: '<botID>:<token>' },
      { key: 'chats', label: 'Chat IDs', required: true, placeholder: 'comma separated' },
    ],
  },
  {
    type: 'pushover', label: 'Pushover', fields: [
      { key: 'token', label: 'API token', secret: true, required: true },
      { key: 'user_key', label: 'User key', secret: true, required: true },
    ],
  },
  {
    type: 'gotify', label: 'Gotify', fields: [
      { key: 'host', label: 'Host', required: true },
      { key: 'token', label: 'App token', secret: true, required: true },
    ],
  },
  { type: 'slack', label: 'Slack', fields: [{ key: 'webhook_url', label: 'Webhook URL', secret: true, required: true }] },
  { type: 'webhook', label: 'Generic webhook', fields: [{ key: 'url', label: 'URL', secret: true, required: true }] },
  {
    type: 'advanced', label: 'Advanced (Shoutrrr URL)', fields: [
      { key: 'url', label: 'Service URL', secret: true, required: true, placeholder: 'scheme://...' },
    ],
  },
];

function emptyForm(): NotifyServiceInput {
  return { name: '', type: 'smtp', enabled: true, config: {} };
}

function formatNextFire(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

export function NotificationSettings() {
  const [payload, setPayload] = useState<NotifySettingsPayload | null>(null);
  const [draft, setDraft] = useState<NotifySettings | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveMessage, setSaveMessage] = useState<{ ok: boolean; text: string } | null>(null);

  // Cron validation state
  const [cronValid, setCronValid] = useState(true);
  const [cronChecking, setCronChecking] = useState(false);
  const [cronError, setCronError] = useState('');
  const [cronNext, setCronNext] = useState<string[]>([]);
  const cronSeq = useRef(0);

  // Services state
  const [services, setServices] = useState<NotifyService[]>([]);
  const [form, setForm] = useState<NotifyServiceInput | null>(null);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSaving, setFormSaving] = useState(false);
  const [serviceError, setServiceError] = useState<string | null>(null);
  const [testing, setTesting] = useState<number | null>(null);
  const [testResults, setTestResults] = useState<Record<number, { ok: boolean; text: string }>>({});
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null);

  const loadServices = async () => {
    const res = await listNotifyServices();
    if (res.success && res.data) {
      setServices(res.data);
    } else {
      setServiceError(res.error || 'Failed to load notification services');
    }
  };

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await getNotifySettings();
        if (cancelled) return;
        if (res.success && res.data) {
          setPayload(res.data);
          setDraft(res.data.settings);
          setCronNext(res.data.next_summaries);
          await loadServices();
        } else if (res.error && /not available/i.test(res.error)) {
          setUnavailable(true);
        } else {
          setLoadError(res.error || 'Failed to load notification settings');
        }
      } catch (err) {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : 'Unknown error');
      }
    })();
    return () => { cancelled = true; };
  }, []);

  // Debounced cron validation; a sequence number discards stale responses.
  const cron = draft?.summary_cron;
  const savedCron = payload?.settings.summary_cron;
  useEffect(() => {
    if (cron === undefined) return;
    const seq = ++cronSeq.current;
    setCronChecking(true);
    const handle = setTimeout(async () => {
      try {
        const res = await validateNotifyCron(cron);
        if (seq !== cronSeq.current) return;
        if (res.success && res.data) {
          setCronValid(res.data.valid);
          setCronError(res.data.error);
          setCronNext(res.data.next);
        } else {
          setCronValid(false);
          setCronError(res.error || 'Could not validate schedule');
          setCronNext([]);
        }
      } catch (err) {
        if (seq !== cronSeq.current) return;
        setCronValid(false);
        setCronError(err instanceof Error ? err.message : 'Could not validate schedule');
        setCronNext([]);
      } finally {
        if (seq === cronSeq.current) setCronChecking(false);
      }
    }, CRON_DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [cron, savedCron]);

  const updateDraft = (patch: Partial<NotifySettings>) => {
    setDraft(d => (d ? { ...d, ...patch } : d));
    setSaveMessage(null);
  };

  const setTiming = (kind: EventKind, timing: NotifyTiming) => {
    setDraft(d => (d ? { ...d, timing: { ...d.timing, [kind]: timing } } : d));
    setSaveMessage(null);
  };

  const saveSettings = async () => {
    if (!draft) return;
    setSaving(true);
    setSaveMessage(null);
    try {
      const res = await updateNotifySettings(draft);
      if (res.success && res.data) {
        setPayload(res.data);
        setDraft(res.data.settings);
        setSaveMessage({ ok: true, text: 'Notification settings saved' });
      } else {
        setSaveMessage({ ok: false, text: res.error || 'Failed to save settings' });
      }
    } catch (err) {
      setSaveMessage({ ok: false, text: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setSaving(false);
    }
  };

  const openAdd = () => {
    setEditingId(null);
    setForm(emptyForm());
    setFormError(null);
  };

  const openEdit = (svc: NotifyService) => {
    setEditingId(svc.id);
    setForm({
      name: svc.name,
      type: svc.type,
      enabled: svc.enabled,
      config: { ...svc.config },
    });
    setFormError(null);
  };

  const closeForm = () => {
    setForm(null);
    setEditingId(null);
    setFormError(null);
  };

  const changeType = (type: string) => {
    // Config keys differ per type, and stored secrets don't carry over.
    setForm(f => (f ? { ...f, type, config: {} } : f));
  };

  const setConfigField = (key: string, value: string) => {
    setForm(f => (f ? { ...f, config: { ...f.config, [key]: value } } : f));
  };

  const submitForm = async () => {
    if (!form) return;
    // Drop empty optional values so the server sees only what was filled in.
    const config: Record<string, string> = {};
    for (const [k, v] of Object.entries(form.config)) {
      if (v.trim() !== '') config[k] = v;
    }
    // A blank secret on edit keeps the stored value (the server swaps MASKED_SECRET back in).
    const original = editingId === null ? undefined : services.find(s => s.id === editingId);
    const typeFields = SERVICE_TYPES.find(t => t.type === form.type)?.fields ?? [];
    if (original && original.type === form.type) {
      for (const f of typeFields) {
        if (f.secret && config[f.key] === undefined && original.config[f.key] === MASKED_SECRET) {
          config[f.key] = MASKED_SECRET;
        }
      }
    }
    setFormSaving(true);
    setFormError(null);
    try {
      const body = { ...form, name: form.name.trim(), config };
      const res = editingId === null ? await createNotifyService(body) : await updateNotifyService(editingId, body);
      if (res.success) {
        await loadServices();
        closeForm();
      } else {
        setFormError(res.error || 'Failed to save service');
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setFormSaving(false);
    }
  };

  const toggleEnabled = async (svc: NotifyService) => {
    setServiceError(null);
    try {
      const res = await updateNotifyService(svc.id, {
        name: svc.name,
        type: svc.type,
        enabled: !svc.enabled,
        config: svc.config, // masked secrets are preserved server-side
      });
      if (res.success) {
        await loadServices();
      } else {
        setServiceError(res.error || 'Failed to update service');
      }
    } catch (err) {
      setServiceError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const sendTest = async (id: number) => {
    setTesting(id);
    setTestResults(r => {
      const next = { ...r };
      delete next[id];
      return next;
    });
    try {
      const res = await testNotifyService(id);
      setTestResults(r => ({
        ...r,
        [id]: res.success ? { ok: true, text: 'Test message sent' } : { ok: false, text: res.error || 'Test failed' },
      }));
      await loadServices();
    } catch (err) {
      setTestResults(r => ({ ...r, [id]: { ok: false, text: err instanceof Error ? err.message : 'Test failed' } }));
    } finally {
      setTesting(null);
    }
  };

  const confirmDelete = async (id: number) => {
    setServiceError(null);
    try {
      const res = await deleteNotifyService(id);
      if (res.success) {
        await loadServices();
      } else {
        setServiceError(res.error || 'Failed to delete service');
      }
    } catch (err) {
      setServiceError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setDeleteTarget(null);
    }
  };

  if (unavailable) {
    return (
      <section className="settings-section">
        <h2 className="section-title">
          <i className="fa-solid fa-bell"></i>
          Notifications
        </h2>
        <div className="settings-card">
          <div className="setting-info">
            <i className="fa-solid fa-circle-info"></i>
            Notifications are not available on this server.
          </div>
        </div>
      </section>
    );
  }

  const typeDef = form ? SERVICE_TYPES.find(t => t.type === form.type) : undefined;
  const formMissing = typeDef && form
    ? typeDef.fields.filter(f => f.required && !(form.config[f.key] ?? '').trim()).map(f => f.label)
    : [];
  const formValid = !!form && form.name.trim() !== '' && formMissing.length === 0;
  const canSaveSettings = !!draft && !saving && !cronChecking && cronValid;

  return (
    <section className="settings-section">
      <h2 className="section-title">
        <i className="fa-solid fa-bell"></i>
        Notifications
      </h2>

      {loadError && (
        <div className="error-banner">
          <i className="fa-solid fa-circle-exclamation"></i>
          {loadError}
        </div>
      )}

      {draft && payload && (
        <div className="settings-card">
          <div className="setting-row">
            <label htmlFor="notify-server-name" className="setting-label">Server name</label>
            <input
              id="notify-server-name"
              type="text"
              className="notify-input"
              value={draft.server_name}
              onChange={(e) => updateDraft({ server_name: e.target.value })}
            />
          </div>
          <div className="setting-row">
            <label htmlFor="notify-base-url" className="setting-label">Base URL</label>
            <input
              id="notify-base-url"
              type="text"
              className="notify-input"
              placeholder="https://docksmith.example.com"
              value={draft.base_url}
              onChange={(e) => updateDraft({ base_url: e.target.value })}
            />
          </div>
          {services.length > 0 && draft.base_url.trim() === '' && (
            <div className="setting-info setting-info-warning" role="status">
              <i className="fa-solid fa-triangle-exclamation"></i>
              <span>Notification links are disabled because no Base URL is set. Set one and save so each item opens the matching container page.</span>
            </div>
          )}
          <div className="setting-row">
            <label htmlFor="notify-min-level" className="setting-label">Minimum level</label>
            <select
              id="notify-min-level"
              className="retention-select"
              value={draft.min_level}
              onChange={(e) => updateDraft({ min_level: e.target.value })}
            >
              {MIN_LEVELS.map(l => (
                <option key={l} value={l}>{l}</option>
              ))}
            </select>
          </div>
          <div className="setting-row">
            <label htmlFor="notify-cron" className="setting-label">Summary schedule (cron)</label>
            <input
              id="notify-cron"
              type="text"
              className={`notify-input monospace ${cronValid ? '' : 'notify-input-invalid'}`}
              value={draft.summary_cron}
              aria-invalid={!cronValid}
              onChange={(e) => updateDraft({ summary_cron: e.target.value })}
            />
          </div>
          <div className="setting-info">
            <i className="fa-solid fa-circle-info"></i>
            Timezone: {payload.timezone || 'unknown'}
          </div>
          {payload.timezone_warning && (
            <div className="setting-info setting-info-error">
              <i className="fa-solid fa-triangle-exclamation"></i>
              {payload.timezone_warning}
            </div>
          )}
          {!cronChecking && !cronValid && (
            <div className="setting-info setting-info-error">
              <i className="fa-solid fa-circle-exclamation"></i>
              {cronError}
            </div>
          )}
          {cronValid && cronNext.length > 0 && (
            <div className="setting-info">
              <i className="fa-solid fa-calendar"></i>
              Next summaries: {cronNext.map(formatNextFire).join(' / ')}
            </div>
          )}

          <h3 className="notify-subtitle">Events</h3>
          <div className="setting-info">
            <i className="fa-solid fa-circle-info"></i>
            Every enabled service receives these events. Immediate events are sent right away; summary events are
            collected and sent together on the schedule above.
          </div>
          {EVENT_KINDS.map(({ kind, label }) => (
            <div className="setting-row" key={kind}>
              <label htmlFor={`notify-timing-${kind}`} className="setting-label">{label}</label>
              <select
                id={`notify-timing-${kind}`}
                className="retention-select"
                value={draft.timing[kind] ?? 'off'}
                onChange={(e) => setTiming(kind, e.target.value as NotifyTiming)}
              >
                <option value="off">Off</option>
                <option value="immediate">Immediate</option>
                <option value="summary">In summary</option>
              </select>
            </div>
          ))}
          <div className="setting-info">
            <i className="fa-solid fa-circle-info"></i>
            Digest changes apply to containers without version tags (e.g. :latest). A container can opt in with the
            docksmith.notify-digest=true label even when this is Off.
          </div>

          <button className="settings-btn" onClick={saveSettings} disabled={!canSaveSettings}>
            <i className="fa-solid fa-floppy-disk"></i>
            <div>
              <div className="btn-title">{saving ? 'Saving...' : 'Save Notification Settings'}</div>
              <div className="btn-description">Applies to all notification services</div>
            </div>
          </button>
          {saveMessage && (
            <div className={`setting-info ${saveMessage.ok ? 'setting-info-success' : 'setting-info-error'}`}>
              <i className={`fa-solid ${saveMessage.ok ? 'fa-check-circle' : 'fa-circle-exclamation'}`}></i>
              {saveMessage.text}
            </div>
          )}
        </div>
      )}

      {draft && (
        <>
          <h3 className="notify-subtitle">Services</h3>
          <div className="settings-card">
            {serviceError && (
              <div className="setting-info setting-info-error">
                <i className="fa-solid fa-circle-exclamation"></i>
                {serviceError}
              </div>
            )}
            {services.length === 0 && (
              <div className="setting-row">
                <span className="setting-value">No notification services configured</span>
              </div>
            )}
            {services.map(svc => (
              <div className="notify-service" key={svc.id}>
                <div className="notify-service-head">
                  <div>
                    <div className="setting-label">{svc.name}</div>
                    <div className="setting-value notify-service-meta">
                      {SERVICE_TYPES.find(t => t.type === svc.type)?.label ?? svc.type}
                    </div>
                  </div>
                  <label className="notify-toggle">
                    <span className="setting-value">Enabled</span>
                    <input
                      type="checkbox"
                      checked={svc.enabled}
                      onChange={() => toggleEnabled(svc)}
                      aria-label={`Enable ${svc.name}`}
                    />
                  </label>
                </div>
                <div className="setting-value notify-service-meta">
                  {svc.last_success_at ? `Last success: ${formatTimeAgo(svc.last_success_at)}` : 'Never delivered'}
                </div>
                {svc.last_error && (
                  <div className="notify-service-error">
                    {svc.last_error_at ? `${formatTimeAgo(svc.last_error_at)}: ` : ''}{svc.last_error}
                  </div>
                )}
                {testResults[svc.id] && (
                  <div className={testResults[svc.id].ok ? 'notify-service-ok' : 'notify-service-error'}>
                    {testResults[svc.id].text}
                  </div>
                )}
                <div className="notify-actions">
                  <button className="notify-btn" onClick={() => sendTest(svc.id)} disabled={testing === svc.id}>
                    {testing === svc.id ? 'Sending...' : 'Send test'}
                  </button>
                  <button className="notify-btn" onClick={() => openEdit(svc)}>Edit</button>
                  {deleteTarget === svc.id ? (
                    <>
                      <button className="notify-btn notify-btn-danger" onClick={() => confirmDelete(svc.id)}>Confirm delete</button>
                      <button className="notify-btn" onClick={() => setDeleteTarget(null)}>Cancel</button>
                    </>
                  ) : (
                    <button className="notify-btn notify-btn-danger" onClick={() => setDeleteTarget(svc.id)}>Delete</button>
                  )}
                </div>
              </div>
            ))}

            {!form && (
              <button className="settings-btn" onClick={openAdd}>
                <i className="fa-solid fa-plus"></i>
                <div>
                  <div className="btn-title">Add Service</div>
                  <div className="btn-description">Email, ntfy, Discord, Telegram, Slack and more</div>
                </div>
              </button>
            )}

            {form && typeDef && (
              <div className="notify-form">
                <h3 className="notify-subtitle">{editingId === null ? 'Add service' : 'Edit service'}</h3>
                <div className="setting-row">
                  <label htmlFor="svc-name" className="setting-label">Name</label>
                  <input
                    id="svc-name"
                    type="text"
                    className="notify-input"
                    value={form.name}
                    onChange={(e) => setForm({ ...form, name: e.target.value })}
                  />
                </div>
                <div className="setting-row">
                  <label htmlFor="svc-type" className="setting-label">Type</label>
                  <select
                    id="svc-type"
                    className="retention-select"
                    value={form.type}
                    onChange={(e) => changeType(e.target.value)}
                  >
                    {SERVICE_TYPES.map(t => (
                      <option key={t.type} value={t.type}>{t.label}</option>
                    ))}
                  </select>
                </div>
                {typeDef.fields.map(field => (
                  <div className="setting-row" key={`${form.type}-${field.key}`}>
                    <label htmlFor={`svc-${field.key}`} className="setting-label">
                      {field.label}{field.required ? ' *' : ''}
                    </label>
                    {field.options ? (
                      <select
                        id={`svc-${field.key}`}
                        className="retention-select"
                        value={form.config[field.key] ?? ''}
                        onChange={(e) => setConfigField(field.key, e.target.value)}
                      >
                        {field.options.map(o => (
                          <option key={o} value={o}>{o === '' ? 'Default' : o}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        id={`svc-${field.key}`}
                        type={field.secret ? 'password' : 'text'}
                        autoComplete="off"
                        className="notify-input"
                        placeholder={field.placeholder}
                        value={form.config[field.key] ?? ''}
                        onChange={(e) => setConfigField(field.key, e.target.value)}
                      />
                    )}
                  </div>
                ))}
                {editingId !== null && typeDef.fields.some(f => f.secret) && (
                  <div className="setting-info">
                    <i className="fa-solid fa-circle-info"></i>
                    Leave {MASKED_SECRET} unchanged to keep the stored secret.
                  </div>
                )}
                <div className="setting-row">
                  <label htmlFor="svc-enabled" className="setting-label">Enabled</label>
                  <input
                    id="svc-enabled"
                    type="checkbox"
                    checked={form.enabled}
                    onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                  />
                </div>
                {formError && (
                  <div className="setting-info setting-info-error">
                    <i className="fa-solid fa-circle-exclamation"></i>
                    {formError}
                  </div>
                )}
                <div className="notify-actions">
                  <button className="notify-btn notify-btn-primary" onClick={submitForm} disabled={!formValid || formSaving}>
                    {formSaving ? 'Saving...' : 'Save service'}
                  </button>
                  <button className="notify-btn" onClick={closeForm} disabled={formSaving}>Cancel</button>
                </div>
              </div>
            )}
          </div>
        </>
      )}
    </section>
  );
}
