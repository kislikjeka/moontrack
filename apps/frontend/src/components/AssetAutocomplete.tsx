import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useQuery } from '@tanstack/react-query';
import { assetService, Asset } from '../services/asset';
import './AssetAutocomplete.css';

interface AssetAutocompleteProps {
  value: string;
  onChange: (asset: Asset | null, symbol: string) => void;
  placeholder?: string;
  className?: string;
  error?: string;
}

/**
 * AssetAutocomplete component - Searchable asset selector with CoinGecko integration
 * Features:
 * - Debounced search (300ms)
 * - Keyboard navigation (up/down arrows, enter)
 * - Loading and error states
 * - Displays symbol, name, and market cap rank
 */
export const AssetAutocomplete: React.FC<AssetAutocompleteProps> = ({
  value,
  onChange,
  placeholder = 'Search asset (e.g., BTC, Bitcoin)',
  className = '',
  error,
}) => {
  const [inputValue, setInputValue] = useState(value);
  const [isOpen, setIsOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const [debouncedQuery, setDebouncedQuery] = useState('');

  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Debounce search query
  useEffect(() => {
    const timer = setTimeout(() => {
      if (inputValue.length >= 2) {
        setDebouncedQuery(inputValue);
      } else {
        setDebouncedQuery('');
      }
    }, 300);

    return () => clearTimeout(timer);
  }, [inputValue]);

  // Fetch search results
  const { data, isLoading, isError } = useQuery({
    queryKey: ['assetSearch', debouncedQuery],
    queryFn: () => assetService.search(debouncedQuery),
    enabled: debouncedQuery.length >= 2,
    staleTime: 24 * 60 * 60 * 1000, // 24 hours - matches backend cache
    retry: 1,
  });

  const assets = data?.assets || [];

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Sync external value changes (only when value prop changes from outside)
  // Use a ref to track if we initiated the change to avoid circular updates
  const isInternalChange = useRef(false);

  useEffect(() => {
    if (!isInternalChange.current) {
      setInputValue(value);
    }
    isInternalChange.current = false;
  }, [value]);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value.toUpperCase();
    isInternalChange.current = true;
    setInputValue(newValue);
    setIsOpen(true);
    setHighlightedIndex(-1);

    // Notify parent of manual input (custom asset)
    onChange(null, newValue);
  };

  const handleSelectAsset = useCallback((asset: Asset) => {
    isInternalChange.current = true;
    setInputValue(asset.symbol);
    setIsOpen(false);
    setHighlightedIndex(-1);
    onChange(asset, asset.symbol);
  }, [onChange]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen || assets.length === 0) {
      if (e.key === 'ArrowDown' && inputValue.length >= 2) {
        setIsOpen(true);
      }
      return;
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setHighlightedIndex((prev) =>
          prev < assets.length - 1 ? prev + 1 : prev
        );
        break;
      case 'ArrowUp':
        e.preventDefault();
        setHighlightedIndex((prev) => (prev > 0 ? prev - 1 : 0));
        break;
      case 'Enter':
        e.preventDefault();
        if (highlightedIndex >= 0 && highlightedIndex < assets.length) {
          handleSelectAsset(assets[highlightedIndex]);
        }
        break;
      case 'Escape':
        setIsOpen(false);
        setHighlightedIndex(-1);
        break;
    }
  };

  const handleFocus = () => {
    if (inputValue.length >= 2) {
      setIsOpen(true);
    }
  };

  const showDropdown = isOpen && inputValue.length >= 2;

  return (
    <div className={`asset-autocomplete ${className}`} ref={containerRef}>
      <input
        ref={inputRef}
        type="text"
        value={inputValue}
        onChange={handleInputChange}
        onKeyDown={handleKeyDown}
        onFocus={handleFocus}
        placeholder={placeholder}
        className={error ? 'error' : ''}
        autoComplete="off"
        aria-autocomplete="list"
        aria-expanded={showDropdown}
        aria-haspopup="listbox"
      />

      {showDropdown && (
        <div className="asset-autocomplete-dropdown" role="listbox">
          {isLoading && (
            <div className="asset-autocomplete-loading">
              <span className="spinner-small"></span>
              <span>Searching...</span>
            </div>
          )}

          {isError && (
            <div className="asset-autocomplete-error">
              Search unavailable. You can still enter an asset symbol manually.
            </div>
          )}

          {!isLoading && !isError && assets.length === 0 && (
            <div className="asset-autocomplete-empty">
              No assets found for "{inputValue}". You can use this symbol for custom assets.
            </div>
          )}

          {!isLoading && !isError && assets.length > 0 && (
            <ul className="asset-autocomplete-list">
              {assets.map((asset, index) => (
                <li
                  key={asset.id}
                  role="option"
                  aria-selected={index === highlightedIndex}
                  className={`asset-autocomplete-item ${
                    index === highlightedIndex ? 'highlighted' : ''
                  }`}
                  onClick={() => handleSelectAsset(asset)}
                  onMouseEnter={() => setHighlightedIndex(index)}
                >
                  <div className="asset-info">
                    <span className="asset-symbol">{asset.symbol}</span>
                    <span className="asset-name">{asset.name}</span>
                    {asset.chain_id && (
                      <span className="asset-chain">{asset.chain_id}</span>
                    )}
                  </div>
                  {asset.market_cap_rank > 0 && (
                    <span className="asset-rank">#{asset.market_cap_rank}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {error && <span className="error-message">{error}</span>}
    </div>
  );
};

export default AssetAutocomplete;
