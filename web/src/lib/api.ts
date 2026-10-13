// Thin wrapper around fetch for the titand API.

export class ApiError extends Error {
	constructor(
		readonly status: number,
		/** Machine-readable reason, e.g. "invalid_credentials". Translated via `error.<code>`. */
		readonly code: string,
		message: string,
		readonly retryAfter = 0
	) {
		super(message);
	}
}

type Body = Record<string, unknown> | Blob | undefined;

async function request<T>(method: string, path: string, body?: Body): Promise<T> {
	const init: RequestInit = { method, credentials: 'same-origin' };
	if (body instanceof Blob) {
		init.body = body;
	} else if (body !== undefined) {
		init.body = JSON.stringify(body);
		init.headers = { 'Content-Type': 'application/json' };
	}

	let res: Response;
	try {
		res = await fetch('/api/v1' + path, init);
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}

	if (!res.ok) {
		let code = 'internal';
		let message = `HTTP ${res.status}`;
		let retryAfter = 0;
		try {
			const data = await res.json();
			code = data.error.code ?? code;
			message = data.error.message ?? message;
			retryAfter = data.error.retryAfter ?? 0;
		} catch {
			// Not a JSON error body, e.g. an answer from a proxy.
		}
		throw new ApiError(res.status, code, message, retryAfter);
	}
	if (res.status === 204 || res.status === 202) return undefined as T;
	return res.json();
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: Body) => request<T>('POST', path, body),
	put: <T>(path: string, body?: Body) => request<T>('PUT', path, body),
	delete: <T>(path: string) => request<T>('DELETE', path)
};

export type Role = 'owner' | 'admin' | 'user';

export type User = { name: string; role: Role };

export type Session = {
	configAvailable: boolean;
	setupRequired: boolean;
	user: User | null;
	hostname: string;
	version?: string;
};

export type SystemInfo = {
	hostname: string;
	version: string;
	kernel: string;
	uptimeSeconds: number;
	cpu: { model: string; threads: number; usagePercent: number };
	memory: { totalBytes: number; usedBytes: number };
	image: { slot: string; sizeBytes: number; overlayUsedBytes: number; overlayTotalBytes: number } | null;
	stick: { label: string; present: boolean };
	network: {
		interface: string;
		ipv4: string;
		gateway: string;
		speedMbps: number;
		rxBytesPerSecond: number;
		txBytesPerSecond: number;
	} | null;
};

export type Preferences = {
	appearance: Record<string, unknown> | null;
	dashboard: Record<string, unknown> | null;
};
