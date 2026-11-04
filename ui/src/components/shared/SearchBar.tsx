import { Tooltip } from './Tooltip';

interface SearchBarProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
}

export function SearchBar({
  value,
  onChange,
  placeholder = 'Search...',
  disabled = false,
  className = '',
}: SearchBarProps) {
  return (
    <div className={`search-bar ${className}`}>
      <i className="fa-solid fa-search" aria-hidden="true"></i>
      <input
        type="text"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="search-input"
        disabled={disabled}
        aria-label={placeholder}
      />
      {value && !disabled && (
        <Tooltip label="Clear search" placement="bottom" align="end">
          <button
            className="clear-search"
            onClick={() => onChange('')}
          >
            <i className="fa-solid fa-times" aria-hidden="true"></i>
          </button>
        </Tooltip>
      )}
    </div>
  );
}
