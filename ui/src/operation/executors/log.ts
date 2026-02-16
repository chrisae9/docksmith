import type { OperationAction } from '../types';
import type { LogEntry } from '../../constants/progress';

export function addLog(dispatch: React.Dispatch<OperationAction>, runId: string, message: string, type: LogEntry['type'] = 'info', icon?: string) {
  dispatch({ type: 'ADD_LOG', runId, entry: { time: Date.now(), message, type, icon } });
}
