import type { ContainerInfo } from '../../types/api';
import { ChangeType } from '../../types/api';
import { isActionable } from '../../utils/status';

interface ContainerRowProps {
  container: ContainerInfo;
  selected: boolean;
  onToggle: () => void;
  onContainerClick: () => void;
  allContainers: ContainerInfo[];
}

export function ContainerRow({ container, selected, onToggle, onContainerClick, allContainers }: ContainerRowProps) {
  const hasAction = isActionable(container.status);
  const hasUpdate = container.status === 'UPDATE_AVAILABLE' || container.status === 'UPDATE_AVAILABLE_BLOCKED' || container.status === 'UP_TO_DATE_PINNABLE';
  const isMismatchStatus = container.status === 'COMPOSE_MISMATCH';
  const isBlocked = container.status === 'UPDATE_AVAILABLE_BLOCKED';

  // Check restart dependencies
  const restartAfter = container.labels?.['docksmith.restart-after'] || '';
  const restartDeps = restartAfter ? restartAfter.split(',').map(d => d.trim()) : [];

  // Find containers that depend on this one
  const dependents = allContainers.filter(c => {
    const deps = c.labels?.['docksmith.restart-after'] || '';
    if (!deps) return false;
    const depList = deps.split(',').map(d => d.trim());
    return depList.includes(container.container_name);
  }).map(c => c.container_name);

  const getStatusIndicator = () => {
    switch (container.status) {
      case 'UPDATE_AVAILABLE':
        if (container.change_type === ChangeType.MajorChange) return <span className="status-badge major" title="Major update">MAJOR</span>;
        if (container.change_type === ChangeType.MinorChange) return <span className="status-badge minor" title="Minor update">MINOR</span>;
        if (container.change_type === ChangeType.PatchChange) return <span className="status-badge patch" title="Patch update">PATCH</span>;
        return <span className="status-badge rebuild" title="Update available">REBUILD</span>;
      case 'UPDATE_AVAILABLE_BLOCKED':
        return <span className="status-badge blocked" title="Update blocked">BLOCKED</span>;
      case 'UP_TO_DATE':
        return <span className="dot current" title="Up to date"></span>;
      case 'UP_TO_DATE_PINNABLE': {
        const pinnableVersion = container.recommended_tag || container.current_version || (container.using_latest_tag ? 'latest' : '(no tag)');
        return <span className="status-badge pin" title={`No version tag specified. Pin to: ${container.image}:${pinnableVersion}`}>PIN</span>;
      }
      case 'LOCAL_IMAGE':
        return <span className="dot local" title="Local image"></span>;
      case 'COMPOSE_MISMATCH': {
        const runningImg = container.image || 'unknown';
        const composeImg = container.compose_image || 'unknown';
        return <span className="status-badge mismatch" title={`Running: ${runningImg}\nCompose: ${composeImg}`}>MISMATCH</span>;
      }
      case 'IGNORED':
        return <span className="dot ignored" title="Ignored"></span>;
      default:
        return <span className="dot" title={container.status}></span>;
    }
  };

  const getVersion = () => {
    // For pinnable containers, show the tag migration path (check this FIRST)
    if (container.status === 'UP_TO_DATE_PINNABLE' && container.recommended_tag) {
      const currentTag = container.using_latest_tag ? 'latest' : (container.current_tag || 'untagged');
      return `${currentTag} → ${container.recommended_tag}`;
    }
    if (hasUpdate && container.latest_version) {
      // Show tag with resolved version in parentheses only when tag doesn't contain version info
      // e.g., "latest (2026.1.29) → latest (2026.2.3)" but "3.14.2-slim → 3.14.3-slim" (no redundant parens)
      const currentTag = container.current_tag || '';
      const currentResolved = container.current_version || '';

      // Build current display: show tag with resolved version in parentheses
      // only if they differ AND the tag doesn't already contain the version
      let currentDisplay: string;
      const currentTagContainsVersion = currentTag && currentResolved && currentTag.includes(currentResolved);
      if (currentTag && currentResolved && currentTag !== currentResolved && !currentTagContainsVersion) {
        currentDisplay = `${currentTag} (${currentResolved})`;
      } else {
        currentDisplay = currentTag || currentResolved || 'current';
      }

      // Build latest display: same logic for the target version
      const latestTag = container.latest_version;
      const latestResolved = container.latest_resolved_version || '';
      let latestDisplay: string;
      const latestTagContainsVersion = latestTag && latestResolved && latestTag.includes(latestResolved);
      if (latestTag && latestResolved && latestTag !== latestResolved && !latestTagContainsVersion) {
        latestDisplay = `${latestTag} (${latestResolved})`;
      } else {
        latestDisplay = latestTag;
      }

      return `${currentDisplay} → ${latestDisplay}`;
    }
    if (container.status === 'LOCAL_IMAGE') {
      return 'Local image';
    }
    if (container.status === 'COMPOSE_MISMATCH') {
      // Show detailed mismatch info: running tag → compose tag
      const runningTag = container.image.includes(':')
        ? container.image.split(':').pop()
        : container.current_tag || 'unknown';
      const composeTag = container.compose_image?.includes(':')
        ? container.compose_image.split(':').pop()
        : container.compose_image || 'unknown';
      return `${runningTag} → ${composeTag}`;
    }
    if (container.status === 'IGNORED') {
      return 'Ignored';
    }
    // Prefer current_version (full version from container labels) over current_tag
    // to match Detail page and show complete version info (e.g., "4.0.16.2944-ls299" not just "4.0.16")
    return container.current_version || container.current_tag || '';
  };

  const handleRowClick = (e: React.MouseEvent) => {
    // Don't open detail modal if clicking checkbox area or left safe zone
    const target = e.target as HTMLElement;
    if (target.closest('.checkbox-area')) return;
    if (target.closest('.selection-zone')) return;
    onContainerClick();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // Don't trigger if focus is on checkbox
    const target = e.target as HTMLElement;
    if (target.tagName === 'INPUT') return;

    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onContainerClick();
    }
  };

  // Get docksmith label settings
  const hasTagRegex = !!container.labels?.['docksmith.tag-regex'];
  const hasPreUpdateScript = !!container.labels?.['docksmith.pre-update-check'];
  const allowsLatest = container.labels?.['docksmith.allow-latest'] === 'true';
  const versionPinMajor = container.labels?.['docksmith.version-pin-major'] === 'true';
  const versionPinMinor = container.labels?.['docksmith.version-pin-minor'] === 'true';
  const versionPinPatch = container.labels?.['docksmith.version-pin-patch'] === 'true';
  const versionPin = versionPinMajor ? 'major' : versionPinMinor ? 'minor' : versionPinPatch ? 'patch' : null;

  // Build accessible description
  const statusLabel = hasUpdate
    ? `Update available: ${container.current_version || container.current_tag} to ${container.latest_version}`
    : container.status.toLowerCase().replace(/_/g, ' ');

  return (
    <li
      className={`${hasUpdate ? 'has-update' : ''} ${isMismatchStatus ? 'has-mismatch' : ''} ${selected ? 'selected' : ''} ${isBlocked ? 'blocked' : ''} container-row-clickable`}
      onClick={handleRowClick}
      onKeyDown={handleKeyDown}
      tabIndex={0}
      role="button"
      aria-label={`${container.container_name}, ${statusLabel}. Press Enter to view details.`}
    >
      <div
        className="selection-zone"
        onClick={(e) => {
          e.stopPropagation();
          // If container has update, toggle selection when clicking the zone
          if (hasAction) {
            if ((e.target as HTMLElement).tagName !== 'INPUT') {
              onToggle();
            }
          }
          // Otherwise just absorb the click (don't navigate)
        }}
      >
        {hasAction && (
          <div className="checkbox-area">
            <input
              type="checkbox"
              checked={selected}
              onChange={onToggle}
              aria-label={`Select ${container.container_name} for ${isMismatchStatus ? 'fix' : 'update'}`}
            />
          </div>
        )}
      </div>
      <div className="container-info">
        <span className="name">{container.container_name}</span>
        <span className="version">{getVersion()}</span>
      </div>
      {getStatusIndicator()}
      {container.pre_update_check_pass && <span className="check" title="Pre-update check passed"><i className="fa-solid fa-check"></i></span>}
      {container.pre_update_check_fail && (
        <span className="warn" title={container.pre_update_check_fail}><i className="fa-solid fa-triangle-exclamation"></i></span>
      )}
      {container.health_status === 'unhealthy' && (
        <span className="warn" title="Container is currently unhealthy"><i className="fa-solid fa-heart-crack"></i></span>
      )}
      {container.health_status === 'starting' && (
        <span className="info" title="Container health check is starting"><i className="fa-solid fa-heartbeat"></i></span>
      )}
      {restartDeps.length > 0 && (
        <span className="info restart-dep" title={`Restarts when ${restartDeps.join(', ')} restart${restartDeps.length > 1 ? '' : 's'}`}>
          <i className="fa-solid fa-link"></i> {restartDeps.length}
        </span>
      )}
      {dependents.length > 0 && (
        <span className="warn restart-dep-by" title={`${dependents.length} container${dependents.length > 1 ? 's' : ''} will restart: ${dependents.join(', ')}`}>
          <i className="fa-solid fa-link"></i> {dependents.length}
        </span>
      )}
      {/* Docksmith label indicators */}
      {versionPin && (
        <span className={`label-icon pin-${versionPin}`} title={`Version pinned to ${versionPin}`}>
          <i className="fa-solid fa-thumbtack"></i>
        </span>
      )}
      {hasTagRegex && (
        <span className="label-icon regex" title="Tag regex filter applied">
          <i className="fa-solid fa-filter"></i>
        </span>
      )}
      {hasPreUpdateScript && !container.pre_update_check_pass && !container.pre_update_check_fail && (
        <span className="label-icon script" title="Pre-update script configured">
          <i className="fa-solid fa-terminal"></i>
        </span>
      )}
      {allowsLatest && (
        <span className="label-icon latest" title="Allows :latest tag">
          <i className="fa-solid fa-tag"></i>
        </span>
      )}
    </li>
  );
}
