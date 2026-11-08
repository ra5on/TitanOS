// PLAN 67: the account picker of the login page. Accounts that logged in on
// this browser are remembered here (name and shown name only, never a
// secret); the NAS may add its own list (GET /login/accounts), which it
// sends only where the owner allows it (home network, never locked
// accounts). The owner's choice (circles or list, show all, also from
// outside) is part of the login look (LoginLook, PUT /login-appearance).
import { api } from '../api.ts';

export type LoginAccount = { name: string; displayName?: string };
export type AccountsStyle = 'circles' | 'list';
export type ServerAccounts = { style: AccountsStyle; accounts: LoginAccount[] };

const KEY = 'titanos.login.accounts';
const MAX = 8;

function sane(value: unknown): LoginAccount | null {
	if (!value || typeof value !== 'object') return null;
	const v = value as Record<string, unknown>;
	if (typeof v.name !== 'string' || !v.name.trim() || v.name.length > 64) return null;
	const out: LoginAccount = { name: v.name.trim() };
	if (typeof v.displayName === 'string' && v.displayName.trim() && v.displayName.length <= 80)
		out.displayName = v.displayName.trim();
	return out;
}

/** The accounts that logged in on this browser, the last one first. */
export function rememberedAccounts(): LoginAccount[] {
	try {
		const list = JSON.parse(localStorage.getItem(KEY) ?? '[]');
		if (!Array.isArray(list)) return [];
		const seen = new Set<string>();
		return list
			.map(sane)
			.filter((a): a is LoginAccount => !!a && !seen.has(a.name) && !!seen.add(a.name))
			.slice(0, MAX);
	} catch {
		return [];
	}
}

function store(list: LoginAccount[]) {
	try {
		localStorage.setItem(KEY, JSON.stringify(list.slice(0, MAX)));
	} catch {
		// No storage (private window): nothing is remembered.
	}
}

/** After a successful login: this account moves to the front. */
export function rememberAccount(name: string, displayName?: string) {
	const account = sane({ name, displayName });
	if (!account) return;
	store([account, ...rememberedAccounts().filter((a) => a.name !== account.name)]);
}

export function forgetAccount(name: string) {
	store(rememberedAccounts().filter((a) => a.name !== name));
}

/**
 * What the NAS shows on its login page, or null when it sends nothing (an
 * older titand, an error, or not allowed from here): then only the accounts
 * of this browser count.
 */
export async function serverAccounts(): Promise<ServerAccounts | null> {
	try {
		const data = await api.get<Partial<ServerAccounts>>('/login/accounts');
		const accounts = Array.isArray(data?.accounts)
			? data.accounts.map(sane).filter((a): a is LoginAccount => !!a)
			: [];
		return { style: data?.style === 'list' ? 'list' : 'circles', accounts };
	} catch {
		return null;
	}
}

/** The browser's accounts first, then those of the NAS not already shown. */
export function mergeAccounts(local: LoginAccount[], server: LoginAccount[]): LoginAccount[] {
	const names = new Set(local.map((a) => a.name));
	const byName = new Map(server.map((a) => [a.name, a]));
	return [
		// The NAS knows the current shown name.
		...local.map((a) => ({ ...a, ...(byName.get(a.name) ?? {}) })),
		...server.filter((a) => !names.has(a.name)),
	];
}

/** "A" for "alex", "MS" for "Maria Schmidt". */
export function initials(account: LoginAccount): string {
	const words = (account.displayName ?? account.name).split(/[\s._-]+/).filter(Boolean);
	const letters =
		words.length > 1 ? words[0][0] + words[words.length - 1][0] : (words[0] ?? '?').slice(0, 1);
	return letters.toUpperCase();
}

/** PLAN 74: the one letter on the account's picture. */
export function firstLetter(account: LoginAccount): string {
	return [...(account.displayName ?? account.name).trim()][0]?.toUpperCase() ?? '?';
}

// PLAN 74: until an account has a picture of its own, it shows a part of one
// of the built-in photos – always the same one for the same name, never a
// plain colour with a letter (owner's decision 8.11.2026).
const FALLBACK = [
	['nordlicht', '45% 30%'],
	['kueste', '20% 70%'],
	['wueste', '60% 60%'],
	['nebelwald', '50% 40%'],
	['bergsee', '50% 45%'],
	['weltraum', '50% 50%'],
] as const;

/** The built-in photo and its part an account without a picture shows. */
export function fallbackPicture(name: string): { url: string; position: string } {
	let hash = 0;
	for (const c of name) hash = (hash * 31 + c.codePointAt(0)!) >>> 0;
	const [id, position] = FALLBACK[hash % FALLBACK.length];
	return { url: `/backgrounds/${id}.webp`, position };
}
