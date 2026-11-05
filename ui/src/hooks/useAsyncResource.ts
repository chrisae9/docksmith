import { useCallback, useEffect, useRef, useState } from 'react';

// Keep results scoped to the input that requested them. Late responses from a
// previous input or an older refresh must never replace the current result.
export function useAsyncResource<T>(key: string, load: () => Promise<T>) {
  const [result, setResult] = useState<{ key: string; data?: T; error: string | null; loading: boolean }>(
    { key, error: null, loading: !!key },
  );
  const requestId = useRef(0);
  if (result.key !== key) setResult({ key, error: null, loading: !!key });

  const request = useCallback(async () => {
    const id = ++requestId.current;
    try {
      const data = await load();
      if (id === requestId.current) setResult(previous => previous.key === key
        ? { key, data, error: null, loading: false } : previous);
    } catch (error) {
      if (id === requestId.current) setResult(previous => previous.key === key
        ? { key, error: error instanceof Error ? error.message : 'Unknown error', loading: false } : previous);
    }
  }, [key, load]);

  const invalidate = useCallback(() => { requestId.current++; }, []);

  useEffect(() => {
    if (key) void request();
    return invalidate;
  }, [key, request, invalidate]);

  const refetch = useCallback(async () => {
    if (!key) return;
    setResult(previous => ({ ...previous, loading: true, error: null }));
    await request();
  }, [key, request]);
  return { ...result, refetch };
}
