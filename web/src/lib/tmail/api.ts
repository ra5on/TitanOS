// 1.05: the requests of T-Mail (docs/TMAIL.md, "API"). Passwords only travel
// in the body of a request; nothing here keeps or logs one.
import { api, ApiError } from '../api.ts';
import { apiPath } from '../cluster-node.ts';
import { lockFrom } from './checklock.svelte.ts';
import type {
	AccountInput,
	Avatars,
	Contact,
	DraftSaved,
	Flags,
	MailAccount,
	MailFolder,
	MailSettings,
	Message,
	MessagePage,
	MessageRow,
	NasFile,
	Provider,
	SearchResult,
	SendMeta,
	TestResult,
} from './types.ts';

const enc = encodeURIComponent;

/**
 * mail_busy (503, Retry-After 5): titand runs two large requests of the
 * account already, or TitanOS has no memory to spare. Tried once more after
 * the pause, then the error stands.
 */
export async function busyOnce<T>(run: () => Promise<T>, wait = sleep): Promise<T> {
	try {
		return await run();
	} catch (err) {
		if (!(err instanceof ApiError) || err.code !== 'mail_busy') throw err;
		await wait((err.retryAfter > 0 ? Math.min(err.retryAfter, 30) : 5) * 1000);
		return run();
	}
}
/**
 * mail_rate_limited may come from any route (connections to servers the
 * account never logged in to, checks): every request notes it, and the
 * pages say how long T-Mail waits (checklock.svelte.ts).
 */
function noteLimits<T extends Record<string, unknown>>(calls: T): T {
	const out: Record<string, unknown> = {};
	for (const [name, call] of Object.entries(calls)) {
		if (typeof call !== 'function') {
			out[name] = call;
			continue;
		}
		out[name] = (...args: unknown[]) => {
			const result = (call as (...a: unknown[]) => unknown)(...args);
			if (result instanceof Promise)
				return result.catch((err: unknown) => {
					lockFrom(err);
					throw err;
				});
			return result;
		};
	}
	return out as T;
}
const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
const acc = (id: string) => `/mail/accounts/${enc(id)}`;

/** A query string of the values that are set (empty strings and false are left out). */
export function query(params: Record<string, string | number | boolean | null | undefined>) {
	const parts = Object.entries(params)
		.filter(([, v]) => v !== undefined && v !== null && v !== '' && v !== false)
		.map(([k, v]) => `${enc(k)}=${enc(v === true ? '1' : String(v))}`);
	return parts.length ? '?' + parts.join('&') : '';
}

export type UnifiedPage = {
	messages: (MessageRow & { account: string; folder: string })[];
	total: number;
	next: string;
	failed: { account: string; code: string }[];
};

/** The filters of 1.12: `since`/`until` are days (YYYY-MM-DD, both counted). */
export type Filters = {
	q?: string;
	unread?: boolean;
	flagged?: boolean;
	from?: string;
	attachment?: boolean;
	since?: string;
	until?: string;
};

export type ListQuery = Filters & {
	folder: string;
	before?: number | null;
	limit?: number;
};

const filterParams = (q: Filters) => ({
	q: q.q,
	unread: q.unread,
	flagged: q.flagged,
	from: q.from,
	attachment: q.attachment,
	since: q.since,
	until: q.until,
});

export const PAGE_SIZE = 50;

export const mailApi = noteLimits({
	accounts: (signal?: AbortSignal) => api.get<MailAccount[]>('/mail/accounts', signal),
	createAccount: (body: AccountInput) =>
		api.post<MailAccount>('/mail/accounts', body as unknown as Record<string, unknown>),
	updateAccount: (id: string, body: AccountInput) =>
		api.put<MailAccount>(acc(id), body as unknown as Record<string, unknown>),
	deleteAccount: (id: string) => api.delete(acc(id)),
	/** Checks the login at IMAP and SMTP; `id` instead of a password uses the stored one. */
	test: (body: Partial<AccountInput> & { id?: string }) =>
		api.post<TestResult>('/mail/accounts/test', body as Record<string, unknown>),
	provider: (email: string) => api.get<Provider>(`/mail/providers${query({ email })}`),
	folders: (id: string, signal?: AbortSignal) =>
		api.get<MailFolder[]>(`${acc(id)}/folders`, signal),
	messages: (id: string, q: ListQuery, signal?: AbortSignal) =>
		api.get<MessagePage>(
			`${acc(id)}/messages${query({
				folder: q.folder,
				before: q.before ?? undefined,
				limit: q.limit ?? PAGE_SIZE,
				...filterParams(q),
			})}`,
			signal,
		),
	/** 1.15 (115-4): "Alle Posteingänge" and "Markiert" over all mailboxes. */
	unified: (
		kind: 'inbox' | 'flagged',
		q: Filters & { cursor?: string | null; limit?: number },
		signal?: AbortSignal,
	) =>
		api.get<UnifiedPage>(
			`/mail/unified${query({ kind, ...filterParams(q), cursor: q.cursor || undefined, limit: q.limit })}`,
			signal,
		),
	/** 1.12: the newest hits of all folders of a mailbox. */
	search: (id: string, q: Filters & { limit?: number }, signal?: AbortSignal) =>
		api.get<SearchResult>(
			`${acc(id)}/search${query({ ...filterParams(q), limit: q.limit })}`,
			signal,
		),
	message: (
		id: string,
		folder: string,
		uid: number,
		opts: { images?: boolean } = {},
		signal?: AbortSignal,
	) =>
		api.get<Message>(
			`${acc(id)}/message${query({ folder, uid, images: opts.images ? 1 : 0 })}`,
			signal,
		),
	/** The address of an attachment for a plain download link. */
	partUrl: (id: string, folder: string, uid: number, part: string) =>
		apiPath(`${acc(id)}/part${query({ folder, uid, part })}`),
	/** The API path of an embedded picture (cid:), for apiFetch. */
	cidPath: (id: string, folder: string, uid: number, cid: string) =>
		`${acc(id)}/cid${query({ folder, uid, cid })}`,
	/** Saves an attachment as the file `path` in `root` (GET /files/roots); it must not exist yet. */
	savePart: (
		id: string,
		body: { folder: string; uid: number; part: string; root: string; path: string },
	) => busyOnce(() => api.post(`${acc(id)}/part/save`, body)),
	flags: (
		id: string,
		body: { folder: string; uids: number[]; add: (keyof Flags)[]; remove: (keyof Flags)[] },
	) => api.post(`${acc(id)}/flags`, body),
	move: (id: string, body: { folder: string; uids: number[]; to: string }) =>
		api.post(`${acc(id)}/move`, body),
	remove: (id: string, body: { folder: string; uids: number[] }) =>
		api.post(`${acc(id)}/delete`, body),
	/** 1.12: into the mailbox's archive (made if there is none). */
	archive: (id: string, body: { folder: string; uids: number[] }) =>
		api.post<{ to: string }>(`${acc(id)}/archive`, body),
	/** 1.12: the own contacts with a picture, for the senders' pictures. */
	avatars: (signal?: AbortSignal) => api.get<Avatars>('/mail/avatars', signal),
	// Large requests (send, draft, saving a part) wait once when titand is busy.
	send: (id: string, form: FormData) => busyOnce(() => api.post(`${acc(id)}/send`, form)),
	draft: (id: string, form: FormData) =>
		busyOnce(() => api.post<DraftSaved>(`${acc(id)}/draft`, form)),
	/** Whether the account may use T-Mail (never 403). */
	access: (signal?: AbortSignal) => api.get<{ enabled: boolean }>('/mail/access', signal),
	contacts: (q: string, signal?: AbortSignal) =>
		api.get<Contact[]>(`/mail/contacts${query({ q })}`, signal),
	settings: () => api.get<MailSettings>('/mail/settings'),
	saveSettings: (body: MailSettings) =>
		api.put<MailSettings>('/mail/settings', body as unknown as Record<string, unknown>),
});

/** The body of /send and /draft: `meta` as JSON, uploaded files as `file`, NAS files as one JSON list `nasFiles`. */
export function sendForm(meta: SendMeta, files: File[], nasFiles: NasFile[]): FormData {
	const form = new FormData();
	form.append('meta', JSON.stringify(meta));
	for (const file of files) form.append('file', file, file.name);
	if (nasFiles.length) form.append('nasFiles', JSON.stringify(nasFiles));
	return form;
}

/** "share/Dokumente:Rechnungen/a.pdf" (the file picker's reference) as {root, path}. */
export function nasFileOf(ref: string): NasFile {
	const at = ref.indexOf(':');
	return at < 0 ? { root: ref, path: '' } : { root: ref.slice(0, at), path: ref.slice(at + 1) };
}
