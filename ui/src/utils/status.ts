/**
 * Checks if a container status indicates it can be updated
 * @param status Container status string
 * @returns true if the container has an available update or can be pinned
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
 * @param status Container status string
 * @returns true if the container has a compose mismatch
 */
export function isMismatch(status: string): boolean {
  return status === 'COMPOSE_MISMATCH';
}

/**
 * Checks if a container can have an action taken (update or fix mismatch)
 * @param status Container status string
 * @returns true if the container can be selected for action
 */
export function isActionable(status: string): boolean {
  return isUpdatable(status) || isMismatch(status);
}
