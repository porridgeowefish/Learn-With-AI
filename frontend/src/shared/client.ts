// Fetch wrapper used by every API call. Centralizes error shape, base URL,
// and AbortController wiring.

import { API_BASE } from '@/shared/lib/constants';

export class ApiError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  // Set to true for endpoints that return plain text instead of JSON.
  rawText?: boolean;
}

// request() dispatches a fetch, throws ApiError on non-2xx, and returns
// parsed JSON (or raw text when rawText:true).
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, headers = {}, signal, rawText = false } = opts;

  const init: RequestInit = {
    method,
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...headers,
    },
    signal,
  };
  if (body !== undefined) {
    init.body = typeof body === 'string' ? body : JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, init);
  } catch (err) {
    // Network / CORS / aborted. Re-throw with a useful message; the caller
    // decides whether to surface a toast or just log it.
    throw new ApiError(0, `network: ${(err as Error).message}`, null);
  }

  const text = await res.text();
  if (!res.ok) {
    let parsed: unknown = null;
    try {
      parsed = text ? JSON.parse(text) : null;
    } catch {
      parsed = { raw: text };
    }
    const detail = parsed && typeof parsed === 'object' && 'error' in parsed ? (parsed as { error: unknown }).error : null;
    const message = typeof detail === 'string' ? detail : detail && typeof detail === 'object' && 'message' in detail && typeof detail.message === 'string' ? detail.message : res.statusText;
    const msg = message || `HTTP ${res.status}`;
    throw new ApiError(res.status, msg, parsed);
  }

  if (rawText) {
    return text as unknown as T;
  }

  if (!text) return null as unknown as T;
  try {
    return JSON.parse(text) as T;
  } catch {
    // Server sent non-JSON in a 2xx — treat as raw text.
    return text as unknown as T;
  }
}

// Convenience verbs — keep call sites readable.
export const http = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: 'GET' }),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: 'PUT', body }),
  delete: <T>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: 'DELETE' }),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: 'PATCH', body }),
};
