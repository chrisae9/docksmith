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
