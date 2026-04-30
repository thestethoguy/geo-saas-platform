'use server';

export interface GeoSearchResult {
  query: string;
  found: number;
  search_time_ms: number;
  results: Record<string, unknown>[];
}

export interface ActionResponse {
  data: GeoSearchResult | null;
  rateLimitRemaining: number | null;
  rateLimitLimit: number | null;
  httpStatus: number;
  error: string | null;
}

export async function searchGeoData(
  apiKey: string,
  searchQuery: string,
): Promise<ActionResponse> {
  if (!apiKey.trim()) {
    return { data: null, rateLimitRemaining: null, rateLimitLimit: null, httpStatus: 0, error: 'API key is required.' };
  }
  if (!searchQuery.trim()) {
    return { data: null, rateLimitRemaining: null, rateLimitLimit: null, httpStatus: 0, error: 'Search query is required.' };
  }

  try {
    const url = `https://geo-api-7ngv.onrender.com/api/v1/search?q=${encodeURIComponent(searchQuery.trim())}`;
    const res = await fetch(url, {
      method: 'GET',
      headers: {
        Authorization: `Bearer ${apiKey.trim()}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
    });

    // Extract rate-limit headers — present on both 2xx and 4xx
    const rlRemaining = res.headers.get('X-Ratelimit-Remaining');
    const rlLimit     = res.headers.get('X-Ratelimit-Limit');
    const payload     = await res.json();

    return {
      data:               res.ok ? (payload as GeoSearchResult) : null,
      rateLimitRemaining: rlRemaining !== null ? parseInt(rlRemaining, 10) : null,
      rateLimitLimit:     rlLimit     !== null ? parseInt(rlLimit, 10)     : null,
      httpStatus:         res.status,
      error:              res.ok ? null : (payload?.error ?? `HTTP ${res.status}`),
    };
  } catch (err) {
    return {
      data: null,
      rateLimitRemaining: null,
      rateLimitLimit: null,
      httpStatus: 0,
      error: err instanceof Error ? err.message : 'Network error — is the Go server running?',
    };
  }
}
