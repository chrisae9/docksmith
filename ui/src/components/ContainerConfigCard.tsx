import type { ContainerInfo } from '../types/api';
import { getRegistryUrl } from '../utils/registry';

interface ContainerConfigCardProps {
  container: ContainerInfo;
  title?: string;
}

/**
 * Shared component for displaying container configuration info.
 * Used by ScriptSelectionPage, TagFilterPage, RestartDependenciesPage.
 */
export function ContainerConfigCard({ container, title = 'Current Configuration' }: ContainerConfigCardProps) {
  const currentTag = container.current_tag || '';
  const imageRef = container.image?.split(':')[0] || '';

  return (
    <section className="config-section">
      <div className="section-header">
        <h2>{title}</h2>
      </div>
      <div className="info-card">
        <div className="info-row">
          <span className="info-label">Container</span>
          <span className="info-value">{container.container_name}</span>
        </div>
        <div className="info-row">
          <span className="info-label">Image</span>
          <span className="info-value">
            {getRegistryUrl(imageRef) ? (
              <a
                href={getRegistryUrl(imageRef)!}
                target="_blank"
                rel="noopener noreferrer"
                className="image-link"
              >
                {imageRef}
                <i className="fa-solid fa-external-link-alt" style={{ marginLeft: '6px', fontSize: '12px', opacity: 0.7 }}></i>
              </a>
            ) : (
              imageRef
            )}
          </span>
        </div>
        {currentTag && (
          <div className="info-row">
            <span className="info-label">Current Tag</span>
            <span className="info-value code">{currentTag}</span>
          </div>
        )}
      </div>
    </section>
  );
}
