// @vitest-environment jsdom
import { StrictMode } from 'react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ContainerPage } from './ContainerPage';
import { getContainerLabels, getContainerStatus, getContainerLogs, inspectContainer } from '../api/client';
import type { APIResponse, ContainerInspect, ContainerInfo, DiscoveryResult } from '../types/api';

vi.mock('../api/client', () => ({
  getContainerLabels: vi.fn(), getContainerStatus: vi.fn(), getContainerLogs: vi.fn(),
  inspectContainer: vi.fn(), recheckContainer: vi.fn(),
}));
vi.mock('../context/EventStreamContext', () => ({ useEventStream: () => ({ reconnecting: false }) }));
vi.mock('../components/Toast', () => ({ useToast: () => ({ error: vi.fn(), success: vi.fn() }) }));

function response<T>(data: T): APIResponse<T> {
  return { success: true, data, timestamp: '2026-11-05T00:00:00Z', version: 'test' };
}
function container(name: string): ContainerInfo {
  return { id: name, container_name: name, image: 'nginx:1.27', status: 'UP_TO_DATE', change_type: 0,
    is_local: false, using_latest_tag: false, using_floating_tag: false, pre_update_check_pass: false, labels: {} };
}
function inspection(name: string): ContainerInspect {
  return { id: name, name, image: 'nginx:1.27', created: '', labels: {}, mounts: [],
    state: { status: 'running', running: true, paused: false, restarting: false, oom_killed: false, dead: false,
      pid: 1, exit_code: 0, error: '', started_at: '', finished_at: '' },
    config: { hostname: name, user: '', env: [], cmd: [], entrypoint: [], working_dir: '', exposed_ports: {}, labels: {} },
    network_settings: { ip_address: '', gateway: '', mac_address: '', ports: {}, networks: {} },
    host_config: { binds: [], network_mode: 'bridge', restart_policy: { name: 'no', maximum_retry_count: 0 },
      port_bindings: {}, memory: 0, memory_swap: 0, cpu_shares: 0, cpu_period: 0, cpu_quota: 0, privileged: false, readonly_rootfs: false },
  };
}
function SwitchContainer() {
  const navigate = useNavigate();
  return <button onClick={() => navigate('/container/second', { state: { tab: 'config' } })}>Switch container</button>;
}
function open(name: string) {
  return render(<StrictMode><MemoryRouter initialEntries={[{ pathname: `/container/${name}`,
    state: { tab: 'config', selectedScript: '/scripts/ready.sh', tagRegex: '^1' } }]}>
    <SwitchContainer /><Routes><Route path="/container/:containerName" element={<ContainerPage />} /></Routes>
  </MemoryRouter></StrictMode>);
}
beforeEach(() => {
  vi.mocked(inspectContainer).mockImplementation(async name => response(inspection(name)));
  vi.mocked(getContainerLabels).mockImplementation(async name => response({ container: name, labels: {} }));
  const status: DiscoveryResult = { containers: ['first', 'second', 'logs'].map(container), standalone_containers: [],
    update_order: [], total_checked: 3, updates_found: 0, up_to_date: 3, local_images: 0, failed: 0, ignored: 0, last_background_run: '' };
  vi.mocked(getContainerStatus).mockResolvedValue(response(status));
});
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.unstubAllGlobals(); });

it('preserves pending subpage edits through repeated StrictMode label responses', async () => {
  const view = open('first');
  await waitFor(() => expect(getContainerLabels).toHaveBeenCalledTimes(2));
  await act(async () => {});
  expect(view.container.querySelector('.precheck-nav-area .nav-value')?.textContent).toContain('ready.sh');
  expect(screen.getByRole('button', { name: 'Save & Restart' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(view.container.querySelector('.precheck-nav-area .nav-value')?.textContent).toContain('None');
  expect(screen.queryByRole('button', { name: 'Save & Restart' })).toBeNull();
});

it('resets cached and editable state when navigating to another container', async () => {
  const view = open('first');
  await waitFor(() => expect(getContainerLabels).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole('button', { name: 'Switch container' }));
  await waitFor(() => expect(getContainerLabels).toHaveBeenLastCalledWith('second'));
  await act(async () => {});
  expect(view.container.querySelector('.precheck-nav-area .nav-value')?.textContent).toContain('None');
  expect(screen.queryByRole('button', { name: 'Save & Restart' })).toBeNull();
});

it('does not start a log stream after leaving the logs tab during an initial request', async () => {
  let finish!: (value: Awaited<ReturnType<typeof getContainerLogs>>) => void;
  vi.mocked(getContainerLogs).mockReturnValue(new Promise(resolve => { finish = resolve; }));
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch);
  let finishInspection!: (value: APIResponse<ContainerInspect>) => void;
  vi.mocked(inspectContainer).mockReturnValue(new Promise(resolve => { finishInspection = resolve; }));
  open('logs');
  await waitFor(() => expect(getContainerLabels).toHaveBeenCalledTimes(2));
  // Label requests can start before the inspection that makes the tabs visible.
  expect(screen.queryByRole('button', { name: 'Logs' })).toBeNull();
  await act(async () => { finishInspection(response(inspection('logs'))); });
  fireEvent.click(await screen.findByRole('button', { name: 'Logs' }));
  expect(getContainerLogs).toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Config' }));
  await act(async () => { finish(response({ container: 'logs', logs: 'late log', tail: '500' })); });
  expect(fetch).not.toHaveBeenCalled();
});
