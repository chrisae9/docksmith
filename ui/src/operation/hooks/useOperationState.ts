import { useReducer } from 'react';
import { operationReducer, createInitialState } from '../reducer';

export function useOperationState() {
  return useReducer(operationReducer, undefined, createInitialState);
}
