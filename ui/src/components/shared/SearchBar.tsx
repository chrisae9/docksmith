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
        <button
          className="clear-search"
          onClick={() => onChange('')}
          aria-label="Clear search"
        >
          <i className="fa-solid fa-times" aria-hidden="true"></i>
        </button>
      )}
    </div>
  );
}
