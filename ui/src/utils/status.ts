/**
 * Checks if a container status indicates it can be updated.
 */
export function isUpdatable(status: string): boolean {
  return (
    status === 'UPDATE_AVAILABLE' ||
    status === 'UPDATE_AVAILABLE_BLOCKED' ||
    status === 'UP_TO_DATE_PINNABLE'
  );
}

/**
 * Checks if a container has a compose mismatch
 */
export function isMismatch(status: string): boolean {
  return status === 'COMPOSE_MISMATCH';
}

/**
 * Checks if a container can have an action taken (update or fix mismatch).
 */
export function isActionable(status: string): boolean {
  return isUpdatable(status) || isMismatch(status);
}
