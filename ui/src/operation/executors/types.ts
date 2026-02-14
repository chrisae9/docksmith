import type { OperationInfo, OperationAction } from '../types';
import type { SetURLSearchParams } from 'react-router-dom';

export interface ExecutorContext {
  dispatch: React.Dispatch<OperationAction>;
  runId: string;
  setSearchParams: SetURLSearchParams;
}

export interface OperationExecutor {
  execute(info: OperationInfo, ctx: ExecutorContext): Promise<void>;
}
