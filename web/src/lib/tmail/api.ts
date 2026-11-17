// 1.05: the requests of T-Mail (docs/TMAIL.md, "API"). Passwords only travel
// in the body of a request; nothing here keeps or logs one.
import { api } from '../api.ts';
import { apiPath } from '../cluster-node.ts';
import type {
	AccountInput,
	Contact,
	Flags,
	MailAccount,
	MailFolder,
	MailSettings,
	Message,
	MessagePage,
	NasFile,
	Provider,
	SendMeta,
	TestResult,
} from './types.ts';

const enc = encodeURIComponent;
const acc = (id: string) => `/mail/accounts/${enc(id)}`;

/** A query string of the values that are set (empty strings and false are left out). */
export function query(params: Record<string, string | number | boolean | null | undefined>) {
	const parts = Object.entries(params)
		.filter(([, v]) => v !== undefined && v !== null && v !== '' && v !== false)
		.map(([k, v]) => `${enc(k)}=${enc(v === true ? '1' : String(v))}`);
	return parts.length ? '?' + parts.join('&') : '';
}

export type ListQuery = {
	folder: string;
	before?: number | null;
	limit?: number;
	q?: string;
	unread?: boolean;
	flagged?: boolean;
};

export const PAGE_SIZE = 50;

export const mailApi = {
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
				q: q.q,
				unread: q.unread,
				flagged: q.flagged,
			})}`,
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
	) => api.post(`${acc(id)}/part/save`, body),
	flags: (
		id: string,
		body: { folder: string; uids: number[]; add: (keyof Flags)[]; remove: (keyof Flags)[] },
	) => api.post(`${acc(id)}/flags`, body),
	move: (id: string, body: { folder: string; uids: number[]; to: string }) =>
		api.post(`${acc(id)}/move`, body),
	remove: (id: string, body: { folder: string; uids: number[] }) =>
		api.post(`${acc(id)}/delete`, body),
	send: (id: string, form: FormData) => api.post(`${acc(id)}/send`, form),
	draft: (id: string, form: FormData) => api.post<{ uid: number }>(`${acc(id)}/draft`, form),
	/** Whether the account may use T-Mail (never 403). */
	access: (signal?: AbortSignal) => api.get<{ enabled: boolean }>('/mail/access', signal),
	contacts: (q: string, signal?: AbortSignal) =>
		api.get<Contact[]>(`/mail/contacts${query({ q })}`, signal),
	settings: () => api.get<MailSettings>('/mail/settings'),
	saveSettings: (body: MailSettings) =>
		api.put<MailSettings>('/mail/settings', body as unknown as Record<string, unknown>),
};

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
