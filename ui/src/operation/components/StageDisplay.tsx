import { getStageIcon } from '../utils';

interface StageDisplayProps {
  currentStage: string | null;
  isComplete: boolean;
  hasErrors: boolean;
  stageMessage: string;
  stageDescription: string;
}

export function StageDisplay({ currentStage, isComplete, hasErrors, stageMessage, stageDescription }: StageDisplayProps) {
  const stageIconClass = getStageIcon(currentStage, isComplete, hasErrors);

  return (
    <div className="progress-stage-section">
      <div className={`stage-icon ${isComplete ? (hasErrors ? 'error' : 'success') : 'in-progress'}`}>
        {!isComplete ? (
          <span className="spinning"><i className={`fa-solid ${stageIconClass}`}></i></span>
        ) : (
          <i className={`fa-solid ${stageIconClass}`}></i>
        )}
      </div>
      <div className="stage-message">{stageMessage}</div>
      {!isComplete && stageDescription && (
        <div className="stage-description">{stageDescription}</div>
      )}
    </div>
  );
}
