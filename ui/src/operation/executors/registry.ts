import type { OperationType } from '../types';
import type { OperationExecutor } from './types';
import { ImmediateExecutor } from './immediateExecutor';
import { BatchImmediateExecutor } from './batchImmediateExecutor';
import { SSETrackedExecutor } from './sseTrackedExecutor';
import { BatchTrackedExecutor } from './batchTrackedExecutor';
import { SequentialExecutor } from './sequentialExecutor';

const executors: Record<OperationType, OperationExecutor> = {
  start: new ImmediateExecutor(),
  stop: new ImmediateExecutor(),
  remove: new ImmediateExecutor(),
  batchStart: new BatchImmediateExecutor(),
  batchStop: new BatchImmediateExecutor(),
  batchRestart: new BatchImmediateExecutor(),
  batchRemove: new BatchImmediateExecutor(),
  batchLabel: new BatchImmediateExecutor(),
  labelRollback: new BatchImmediateExecutor(),
  restart: new SSETrackedExecutor(),
  rollback: new SSETrackedExecutor(),
  fixMismatch: new SSETrackedExecutor(),
  stackRestart: new SSETrackedExecutor(),
  update: new BatchTrackedExecutor(),
  mixed: new BatchTrackedExecutor(),
  stackStop: new SequentialExecutor(),
  batchFixMismatch: new SequentialExecutor(),
};

export function getExecutor(type: OperationType): OperationExecutor {
  return executors[type];
}
