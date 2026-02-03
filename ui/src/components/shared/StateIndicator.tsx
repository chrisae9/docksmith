interface StateIndicatorProps {
  state: string;
  healthStatus?: string;
  isLoading?: boolean;
  className?: string;
}

export function StateIndicator({
  state,
  healthStatus,
  isLoading = false,
  className = '',
}: StateIndicatorProps) {
  const getStateClass = () => {
    if (isLoading) return 'loading';
    switch (state) {
      case 'running':
        if (healthStatus === 'unhealthy') return 'unhealthy';
        if (healthStatus === 'starting') return 'starting';
        return 'running';
      case 'paused':
        return 'paused';
      case 'exited':
      case 'dead':
        return 'stopped';
      case 'restarting':
        return 'restarting';
      default:
        return '';
    }
  };

  return (
    <span className={`state-indicator ${getStateClass()} ${className}`}>
      {isLoading && <i className="fa-solid fa-circle-notch fa-spin"></i>}
    </span>
  );
}
