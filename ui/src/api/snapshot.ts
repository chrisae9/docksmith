import type { ContainerInspect, DiscoveryResult, ExplorerData } from '../types/api';

interface Snapshot {
  explorer?: ExplorerData;
  status?: DiscoveryResult;
  updatedAt: number;
}

let snapshot: Snapshot | null = null;
const inspects = new Map<string, ContainerInspect>();

export function getSnapshot(): Snapshot | null {
  return snapshot;
}

export function setSnapshot(partial: { explorer?: ExplorerData; status?: DiscoveryResult }): void {
  snapshot = { ...snapshot, ...partial, updatedAt: Date.now() };
}

export function getCachedInspect(name: string): ContainerInspect | null {
  return inspects.get(name) ?? null;
}

export function setCachedInspect(name: string, data: ContainerInspect): void {
  inspects.set(name, data);
}
