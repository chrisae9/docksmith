import { ContainerProgressRow } from '../../components/ProgressComponents';
import type { ContainerState } from '../types';

interface ContainerListProps {
  containers: ContainerState[];
  expectedDependents: string[];
  dependentsRestarted: string[];
  dependentsBlocked: string[];
  currentStage: string | null;
}

export function ContainerList({ containers, expectedDependents, dependentsRestarted, dependentsBlocked, currentStage }: ContainerListProps) {
  return (
    <section className="containers-section">
      <h2><i className="fa-solid fa-cube"></i> Containers</h2>
      <div className="container-list">
        {containers.map((container) => (
          <ContainerProgressRow key={container.name} container={container} />
        ))}

        {/* Dependent Containers Section */}
        {(dependentsRestarted.length > 0 || dependentsBlocked.length > 0 || (currentStage === 'restarting_dependents' && expectedDependents.length > 0)) && (
          <div className="dependents-section">
            <h3 className="dependents-header">
              <i className="fa-solid fa-link"></i>
              Dependent Containers
            </h3>
            {dependentsRestarted.map((depName) => (
              <div key={depName} className="container-item status-success dependent">
                <div className="container-main-row">
                  <span className="status-icon">
                    <i className="fa-solid fa-check"></i>
                  </span>
                  <span className="container-name">{depName}</span>
                  <span className="container-badge dependent">Dependent</span>
                </div>
                <div className="container-message">Restarted successfully</div>
              </div>
            ))}
            {dependentsBlocked.map((depName) => (
              <div key={depName} className="container-item status-failed dependent">
                <div className="container-main-row">
                  <span className="status-icon">
                    <i className="fa-solid fa-xmark"></i>
                  </span>
                  <span className="container-name">{depName}</span>
                  <span className="container-badge dependent warning">Blocked</span>
                </div>
                <div className="container-message">Pre-update check failed</div>
              </div>
            ))}
            {expectedDependents
              .filter(d => !dependentsRestarted.includes(d) && !dependentsBlocked.includes(d))
              .map((depName) => (
              <div key={depName} className="container-item status-in_progress dependent">
                <div className="container-main-row">
                  <span className="status-icon">
                    <i className="fa-solid fa-spinner fa-spin"></i>
                  </span>
                  <span className="container-name">{depName}</span>
                  <span className="container-badge dependent">Dependent</span>
                </div>
                <div className="container-message">Restarting...</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
