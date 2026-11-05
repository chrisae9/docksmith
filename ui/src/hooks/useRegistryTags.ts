import { useCallback } from 'react';
import { getRegistryTags } from '../api/client';
import { useAsyncResource } from './useAsyncResource';

export function useRegistryTags(imageRef: string) {
  const load = useCallback(async () => {
    const response = await getRegistryTags(imageRef);
    if (!response.success || !response.data) throw new Error(response.error || 'Failed to fetch tags');
    return response.data.tags || [];
  }, [imageRef]);
  const { data, loading, error, refetch } = useAsyncResource(imageRef, load);
  return { tags: data || [], loading, error, refetch };
}
