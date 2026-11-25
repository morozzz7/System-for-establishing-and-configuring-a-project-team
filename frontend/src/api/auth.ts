/**
 * Local auth provider client. In HTTP mode a missing/expired token is obtained lazily via
 * POST /auth/login using configured synthetic credentials, then sent as a bearer token.
 * The server derives the acting principal from the token; no UI code asserts an identity.
 */
export interface LoginResponse {
  access_token: string;
  token_type: 'Bearer';
  expires_in: number;
  actor: string;
}

const TOKEN_KEY = 'teamopt.access_token';
const API_BASE = import.meta.env.VITE_API_BASE_URL as string | undefined;

const configuredLogin = (import.meta.env.VITE_AUTH_LOGIN as string | undefined) ?? 'manager';
const configuredPassword = (import.meta.env.VITE_AUTH_PASSWORD as string | undefined) ?? 'demo';

let inFlight: Promise<string> | null = null;
let memoryToken: string | null = null;

export function getToken(): string | null {
  if (memoryToken) return memoryToken;
  try {
    return globalThis.localStorage?.getItem(TOKEN_KEY) ?? null;
  } catch {
    return null;
  }
}

export function clearToken(): void {
  memoryToken = null;
  try {
    globalThis.localStorage?.removeItem(TOKEN_KEY);
  } catch {
    // Storage can be unavailable in private browsing; the next request re-authenticates.
  }
}

export async function login(): Promise<LoginResponse> {
  if (!API_BASE) {
    return { access_token: 'mock-token', token_type: 'Bearer', expires_in: 3600, actor: 'emp_mgr01' };
  }
  const response = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ login: configuredLogin, password: configuredPassword }),
  });
  if (!response.ok) {
    throw new Error(`Authentication failed (${response.status})`);
  }
  const payload = (await response.json()) as LoginResponse;
  memoryToken = payload.access_token;
  try {
    globalThis.localStorage?.setItem(TOKEN_KEY, payload.access_token);
  } catch {
    // Memory keeps the token for this session when storage is unavailable.
  }
  return payload;
}

/** Return a valid bearer token, de-duplicating concurrent logins. */
export async function ensureToken(): Promise<string | null> {
  if (!API_BASE) return null;
  const existing = getToken();
  if (existing) return existing;
  if (!inFlight) {
    inFlight = login()
      .then((payload) => payload.access_token)
      .finally(() => {
        inFlight = null;
      });
  }
  return inFlight;
}
