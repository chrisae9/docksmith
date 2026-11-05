// @vitest-environment jsdom
import { StrictMode } from 'react';
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useAsyncResource } from './useAsyncResource';
import { useElapsedTime } from './useElapsedTime';
import { useEventStreamCore, type UpdateProgressEvent } from './useEventStream';
import { useSSEProgress } from '../operation/hooks/useSSEProgress';
import { createInitialState } from '../operation/reducer';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

describe('request ownership', () => {
  it('clears old input data and ignores an older response that arrives last', async () => {
    const old = deferred<string>();
    const current = deferred<string>();
    const load = vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
    const hook = renderHook(({ input }) => useAsyncResource(input, load), { initialProps: { input: 'old' } });
    hook.rerender({ input: 'current' });
    expect(hook.result.current.loading).toBe(true);
    expect(hook.result.current.data).toBeUndefined();
    await act(async () => { current.resolve('current result'); });
    await act(async () => { old.resolve('stale result'); });
    expect(hook.result.current.data).toBe('current result');
    expect(hook.result.current.loading).toBe(false);
  });

  it('lets a refresh supersede an initial request, including errors', async () => {
    const first = deferred<string>();
    const refresh = deferred<string>();
    const load = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(refresh.promise);
    const hook = renderHook(() => useAsyncResource('image', load));
    let done!: Promise<void>;
    act(() => { done = hook.result.current.refetch(); });
    await act(async () => { refresh.resolve('new tags'); await done; });
    await act(async () => { first.reject(new Error('late failure')); });
    expect(hook.result.current.data).toBe('new tags');
    expect(hook.result.current.error).toBeNull();
  });

  it('does not request missing inputs and clears data when input is removed', async () => {
    const load = vi.fn().mockResolvedValue('result');
    const hook = renderHook(({ input }) => useAsyncResource(input, load), { initialProps: { input: '' } });
    expect(load).not.toHaveBeenCalled();
    expect(hook.result.current.loading).toBe(false);
    await act(async () => { hook.rerender({ input: 'valid' }); });
    expect(hook.result.current.data).toBe('result');
    hook.rerender({ input: '' });
    expect(hook.result.current.data).toBeUndefined();
    expect(hook.result.current.loading).toBe(false);
  });

  it('survives StrictMode effect cleanup and retries an error', async () => {
    const load = vi.fn().mockRejectedValue(new Error('offline'));
    const hook = renderHook(() => useAsyncResource('key', load), { wrapper: StrictMode });
    await act(async () => {});
    expect(hook.result.current.error).toBe('offline');
    load.mockResolvedValue('recovered');
    await act(async () => { await hook.result.current.refetch(); });
    expect(hook.result.current.data).toBe('recovered');
    expect(hook.result.current.error).toBeNull();
  });
});

describe('operation clock', () => {
  it('shares one timer and releases it after the last subscriber unmounts', () => {
    vi.useFakeTimers(); vi.setSystemTime(10000);
    const first = renderHook(() => useElapsedTime(10000, true));
    const second = renderHook(() => useElapsedTime(10000, true));
    expect(first.result.current).toBe(0);
    expect(vi.getTimerCount()).toBe(1);
    act(() => { vi.advanceTimersByTime(2000); });
    expect(first.result.current).toBe(2); expect(second.result.current).toBe(2);
    first.unmount(); expect(vi.getTimerCount()).toBe(1);
    second.unmount(); expect(vi.getTimerCount()).toBe(0);
  });

  it('keeps final time fixed, resets for a new run, and never starts negative', () => {
    vi.useFakeTimers(); vi.setSystemTime(10500);
    const hook = renderHook<number, { start: number | null; active: boolean; end: number | null }>(({ start, active, end }) =>
      useElapsedTime(start, active, end), { initialProps: { start: 10500, active: true, end: null } });
    expect(hook.result.current).toBe(0);
    hook.rerender({ start: 5000, active: false, end: 9000 });
    expect(hook.result.current).toBe(4); expect(vi.getTimerCount()).toBe(0);
    act(() => { vi.advanceTimersByTime(3000); });
    expect(hook.result.current).toBe(4);
    hook.rerender({ start: null, active: false, end: null });
    expect(hook.result.current).toBe(0);
  });
});

class FakeEventSource extends EventTarget {
  static instances: FakeEventSource[] = [];
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;
  constructor() { super(); FakeEventSource.instances.push(this); }
  close() { this.closed = true; }
}

it('retains a burst of SSE events, drains once, and cancels reconnection on unmount', () => {
  vi.useFakeTimers(); FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
  const hook = renderHook(useEventStreamCore);
  const source = FakeEventSource.instances[0];
  act(() => {
    for (let i = 0; i < 10; i++) source.dispatchEvent(new MessageEvent('update.progress', {
      data: JSON.stringify({ payload: { operation_id: 'operation', container_name: 'app', stage: 'pulling', progress: i } }),
    }));
  });
  expect(hook.result.current.eventSeq).toBe(10);
  expect(hook.result.current.drainEvents()).toHaveLength(10);
  expect(hook.result.current.drainEvents()).toEqual([]);
  act(() => { source.onerror?.(new Event('error')); });
  expect(vi.getTimerCount()).toBe(1);
  hook.unmount(); expect(source.closed).toBe(true); expect(vi.getTimerCount()).toBe(0);
});

it('processes queued SSE events against the current run instead of stale render state', () => {
  let state = { ...createInitialState(), phase: 'running' as const, operationId: 'first' };
  let queue: UpdateProgressEvent[] = [];
  const drain = () => queue.splice(0);
  const dispatch = vi.fn();
  const hook = renderHook(({ seq }) => useSSEProgress(state, dispatch, drain, seq), { initialProps: { seq: 0 } });
  state = { ...state, operationId: 'second' };
  queue = [{ operation_id: 'second', container_name: 'app', stack_name: '', stage: 'pulling', progress: 20, percent: 20, message: 'Pulling' }];
  hook.rerender({ seq: 1 });
  expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({ type: 'SSE_EVENT', event: expect.objectContaining({ operation_id: 'second' }) }));
  expect(queue).toEqual([]);
});

it('drains an early SSE event when the operation becomes running without a new event', () => {
  let state = createInitialState();
  const queued: UpdateProgressEvent[] = [{ operation_id: 'early', container_name: 'app', stack_name: '',
    stage: 'pulling', progress: 10, percent: 10, message: 'Starting' }];
  const drain = () => queued.splice(0);
  const dispatch = vi.fn();
  const hook = renderHook(() => useSSEProgress(state, dispatch, drain, 1));
  expect(dispatch).not.toHaveBeenCalled();
  state = { ...state, phase: 'running', operationId: 'early' };
  hook.rerender();
  expect(dispatch).toHaveBeenCalledWith(expect.objectContaining({ type: 'SSE_EVENT' }));
  expect(queued).toEqual([]);
});
