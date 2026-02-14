interface OperationHeaderProps {
  title: string;
  isComplete: boolean;
  onBack: () => void;
}

export function OperationHeader({ title, isComplete, onBack }: OperationHeaderProps) {
  return (
    <header className="page-header">
      <button className="back-button" onClick={onBack} disabled={!isComplete}>
        &larr; Back
      </button>
      <h1>{title}</h1>
      <div className="header-spacer" />
    </header>
  );
}
