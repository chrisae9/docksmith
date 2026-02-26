import { useState, useEffect, useCallback } from 'react';
import { getRegistryTags } from '../api/client';

interface UseRegistryTagsReturn {
  tags: string[];
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

/**
 * Hook to fetch tags for a Docker image from the registry (cached)
 * Returns tags sorted by version (descending) where possible
 */
export function useRegistryTags(imageRef: string): UseRegistryTagsReturn {
  const [tags, setTags] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTags = useCallback(async () => {
    if (!imageRef) {
      setTags([]);
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await getRegistryTags(imageRef);

      if (response.success && response.data) {
        setTags(response.data.tags || []);
      } else {
        setError(response.error || 'Failed to fetch tags');
        setTags([]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
      setTags([]);
    } finally {
      setLoading(false);
    }
  }, [imageRef]);

  useEffect(() => {
    fetchTags();
  }, [fetchTags]);

  return {
    tags,
    loading,
    error,
    refetch: fetchTags,
  };
}
