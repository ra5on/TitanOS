// Gruppe „Dokumente und Fristen“ (PLAN 54-D): Dokumenten-Gedächtnis,
// Fristenwächter, Papierkram-Autopilot. Everything is off by default.
import { api } from '../api.ts';

export type DocsNAS = { enabled?: boolean; deadlines?: boolean; paper?: boolean; at?: string };
export type FolderRef = { root: string; path: string };
export type DocsMine = {
	on?: boolean;
	custom?: boolean;
	folders?: FolderRef[];
	deadlines?: boolean;
	mode?: '' | 'suggest' | 'remind';
	lead?: number[];
	noScans?: boolean;
};
export type PaperMail = {
	enabled?: boolean;
	host?: string;
	port?: number;
	security?: '' | 'tls' | 'starttls';
	user?: string;
	/** Only sent when changed; the server never sends it back. */
	password?: string;
	passwordSet?: boolean;
	mode?: '' | 'both' | 'attachments';
};
export type PaperMine = {
	on?: boolean;
	filing?: '' | 'auto' | 'ask';
	scheme?: '' | 'sender' | 'type' | 'year';
	target?: FolderRef;
	mail?: PaperMail;
};

export type DocsFolderState = 'new' | 'done' | 'running' | 'asleep' | 'noAccess' | 'full' | 'error';
export type DocsFolder = FolderRef & {
	files: number;
	pending?: number;
	lastRun?: string;
	state: DocsFolderState;
	percent?: number;
};
export type PaperInboxInfo = {
	folder: string;
	waiting: number;
	ask: number;
	mailAt?: string;
	mailError?: string;
};
export type DocsStatus = {
	folders: DocsFolder[];
	running: boolean;
	at: string;
	paper?: PaperInboxInfo | null;
};

export const PAPER_TYPES = [
	'energy',
	'telecom',
	'insurance',
	'shopping',
	'health',
	'housing',
	'tax',
	'other',
] as const;
export type PaperType = (typeof PAPER_TYPES)[number];

export type PaperEntry = {
	id: string;
	name: string;
	state: 'filed' | 'ask' | 'waiting';
	origin: 'inbox' | 'mail' | 'mailtext';
	to?: string;
	root: string;
	path: string;
	at: string;
	sender?: string;
	type: PaperType;
	kind?: string;
	cents?: number;
	due?: string;
	doubt?: PaperType[];
	paid?: boolean;
	error?: string;
	subject?: string;
};
export type PaperSummary = {
	month: string;
	months: string[];
	total: number;
	count: number;
	prevTotal: number;
	types: { type: PaperType; cents: number; count: number; senders: string[] }[];
	open: PaperEntry[];
	recent: PaperEntry[];
	mailAt?: string;
	mailError?: string;
};

export const docsStatus = () => api.get<DocsStatus>('/agent/docs');
export const docsRun = () => api.post('/agent/docs/run');
export const paperSummary = (month = '') =>
	api.get<PaperSummary>('/agent/paper' + (month ? `?month=${encodeURIComponent(month)}` : ''));
export const paperRun = () => api.post('/agent/paper/run');
export const paperMailTest = () => api.post('/agent/paper/mail/test');
export const paperUndo = (id: string) => api.post<PaperEntry>(`/agent/paper/${id}/undo`, {});
export const paperAssign = (
	id: string,
	body: { sender: string; type: PaperType; kind?: string; date?: string },
) => api.post<PaperEntry>(`/agent/paper/${id}/assign`, body);
export const paperPaid = (id: string, paid = true) =>
	api.post<PaperEntry>(`/agent/paper/${id}/paid`, { paid });

/** Cents the German or English way: 1.284,37 €. */
export function euro(cents: number, locale: string): string {
	return new Intl.NumberFormat(locale === 'de' ? 'de-DE' : 'en-GB', {
		style: 'currency',
		currency: 'EUR',
	}).format(cents / 100);
}

/** A folder for people: "KI-Agent › Dokumente". */
export function place(f: FolderRef): string {
	const share = f.root.replace(/^share\//, '');
	return f.path ? `${share} › ${f.path.split('/').join(' › ')}` : share;
}

/** Link of the file manager to the folder of a file. */
export function fileLink(root: string, path: string): string {
	const dir = path.split('/').slice(0, -1).join('/');
	return `/files?root=${encodeURIComponent(root)}&path=${encodeURIComponent(dir)}`;
}
