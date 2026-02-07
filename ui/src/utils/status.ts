interface EnvAware {
  status: string;
  env_controlled?: boolean;
}

/**
 * Checks if a container status indicates it can be updated.
 * Accepts a status string or a container object. When given a container,
 * env-controlled pinnable containers are excluded (nothing docksmith can do).
 */
export function isUpdatable(statusOrContainer: string | EnvAware): boolean {
  const status = typeof statusOrContainer === 'string' ? statusOrContainer : statusOrContainer.status;
  const envControlled = typeof statusOrContainer === 'object' ? statusOrContainer.env_controlled : false;

  if (status === 'UP_TO_DATE_PINNABLE' && envControlled) return false;

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
 * Accepts a status string or a container object.
 */
export function isActionable(statusOrContainer: string | EnvAware): boolean {
  const status = typeof statusOrContainer === 'string' ? statusOrContainer : statusOrContainer.status;
  return isUpdatable(statusOrContainer) || isMismatch(status);
}
