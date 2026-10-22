// The chat with the assistant: open or closed, the chats of the account
// and the one that is open. Chats are kept by the NAS (on a volume); the
// browser only shows them. Actions the assistant proposes run here, with
// the session of the account, once the person confirms them.
import { api, ApiError } from '#lib/api.ts';

export type ChatAction = {
	id: string;
	tool: string;
	key: string;
	params?: Record<string, string>;
	reason: string;
	level: 'normal' | 'danger' | 'destroy';
	/** What has to be typed for a destroying action, and where it goes. */
	confirm?: string;
	confirmField?: string;
	method: 'GET' | 'POST' | 'PUT' | 'DELETE';
	path: string;
	body?: Record<string, unknown>;
	/** Tools from outside TitanOS the model read before it proposed this. */
	after?: string[];
	state: 'pending' | 'done' | 'failed' | 'rejected' | 'expired';
	result?: string;
};
export type ChatAttachment = { name: string; kind: 'text' | 'image'; text?: string; image?: string; size?: number };
export type ChatSource = { title: string; url: string };
export type ChatEntry = {
	role: 'user' | 'assistant' | 'tool';
	content: string;
	time?: string;
	thinking?: string;
	attachments?: ChatAttachment[];
	page?: string;
	calls?: { id: string; name: string; args: unknown }[];
	stopped?: boolean;
	problem?: { code: string; detail?: string };
	sources?: ChatSource[];
	tool?: string;
	args?: unknown;
	ok?: boolean;
	action?: ChatAction;
};
export type ChatSummary = { id: string; title: string; created: string; updated: string; messages: number };
export type AssistantStatus = { available: boolean; enabled: boolean; model: string; web: boolean; persistent: boolean };
export type AssistantModel = { name: string; sizeBytes: number; parameters?: string; quantization?: string; capabilities: string[]; contextLength?: number };
export type AssistantSettings = {
	enabled: boolean;
	ollama: string;
	model: string;
	users: boolean;
	web: boolean;
	webUsers: boolean;
	volume: string;
	prompt: string;
	numCtx: number;
	temperature: number | null;
};
export type AssistantPrefs = { floating: boolean; width: number; pageContext: boolean; model: string };

const DEFAULT_PREFS: AssistantPrefs = { floating: true, width: 440, pageContext: true, model: '' };

export const assistant = $state({
	open: false,
	/** The panel shows the open chat or the list of chats. */
	view: 'chat' as 'chat' | 'list',
	/** null until read: whether this account may chat at all. */
	status: null as AssistantStatus | null,
	models: [] as AssistantModel[],
	chats: [] as ChatSummary[],
	search: '',
	chatId: '',
	title: '',
	messages: [] as ChatEntry[],
	busy: false,
	/** The answer that is being written right now. */
	draft: { content: '', thinking: '' },
	error: '',
	prefs: { ...DEFAULT_PREFS } as AssistantPrefs
});

let controller: AbortController | null = null;

export function useAssistantPrefs(value: Record<string, unknown> | null) {
	const v = value ?? {};
	assistant.prefs = {
		floating: typeof v.floating === 'boolean' ? v.floating : DEFAULT_PREFS.floating,
		width: typeof v.width === 'number' && v.width >= 320 && v.width <= 1200 ? v.width : DEFAULT_PREFS.width,
		pageContext: typeof v.pageContext === 'boolean' ? v.pageContext : DEFAULT_PREFS.pageContext,
		model: typeof v.model === 'string' ? v.model : ''
	};
}

let saveTimer: ReturnType<typeof setTimeout> | undefined;
export function setPrefs(change: Partial<AssistantPrefs>) {
	assistant.prefs = { ...assistant.prefs, ...change };
	clearTimeout(saveTimer);
	saveTimer = setTimeout(() => {
		api.put('/preferences/assistant', { ...assistant.prefs }).catch(() => {
			// Not saved: it holds until the page is loaded again.
		});
	}, 500);
}

const problemOf = (err: unknown) => (err instanceof ApiError ? err.code : 'internal');

export async function loadAssistant() {
	try {
		assistant.status = await api.get<AssistantStatus>('/assistant/status');
	} catch {
		assistant.status = { available: false, enabled: false, model: '', web: false, persistent: false };
	}
	if (!assistant.status.available) assistant.open = false;
}

export async function loadModels() {
	try {
		assistant.models = (await api.get<{ models: AssistantModel[] }>('/assistant/models')).models;
	} catch {
		assistant.models = [];
	}
}

/** The model of this account, or the default of the NAS. */
export function chosenModel(): string {
	return assistant.prefs.model || assistant.status?.model || '';
}

export async function loadChats() {
	try {
		const query = assistant.search.trim() ? `?search=${encodeURIComponent(assistant.search.trim())}` : '';
		const res = await api.get<{ chats: ChatSummary[]; persistent: boolean }>('/assistant/chats' + query);
		assistant.chats = res.chats;
		if (assistant.status) assistant.status.persistent = res.persistent;
		assistant.error = '';
	} catch (err) {
		assistant.error = problemOf(err);
	}
}

export function openAssistant() {
	if (!assistant.status?.available) return;
	assistant.open = true;
	if (assistant.models.length === 0) loadModels();
	if (!assistant.chatId) {
		assistant.view = 'list';
		loadChats().then(() => {
			// Start where the newest chat is, or with an empty one.
			if (assistant.chats.length > 0) openChat(assistant.chats[0].id);
			else newChat();
		});
	}
}

export function toggleAssistant() {
	if (assistant.open) assistant.open = false;
	else openAssistant();
}

export async function openChat(id: string) {
	if (assistant.busy) return;
	try {
		const res = await api.get<{ chat: { id: string; title: string; messages: ChatEntry[] }; busy: boolean }>(`/assistant/chats/${id}`);
		assistant.chatId = res.chat.id;
		assistant.title = res.chat.title;
		assistant.messages = res.chat.messages;
		assistant.view = 'chat';
		assistant.error = '';
	} catch (err) {
		assistant.error = problemOf(err);
	}
}

/** Starts an empty chat; the NAS creates it with the first message. */
export function newChat() {
	if (assistant.busy) return;
	assistant.chatId = '';
	assistant.title = '';
	assistant.messages = [];
	assistant.view = 'chat';
	assistant.error = '';
}

export async function renameChat(id: string, title: string) {
	await api.put(`/assistant/chats/${id}`, { title });
	if (id === assistant.chatId) assistant.title = title;
	await loadChats();
}

export async function deleteChat(id: string) {
	await api.delete(`/assistant/chats/${id}`);
	if (id === assistant.chatId) newChat();
	await loadChats();
}

export async function deleteAllChats() {
	await api.delete('/assistant/chats');
	newChat();
	await loadChats();
}

function currentPage(): string {
	if (!assistant.prefs.pageContext || typeof location === 'undefined') return '';
	const page = location.pathname + location.search;
	return /^\/[A-Za-z0-9/_.~?=&%+-]{0,199}$/.test(page) && !page.startsWith('/assistant') ? page : '';
}

/** Sends to a route of the chat and follows what comes back. */
async function stream(path: string, body: Record<string, unknown>) {
	assistant.busy = true;
	assistant.draft = { content: '', thinking: '' };
	assistant.error = '';
	controller = new AbortController();
	try {
		const res = await fetch('/api/v1' + path, {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(body),
			signal: controller.signal
		});
		if (!res.ok || !res.body) {
			let code = 'internal';
			try {
				code = (await res.json()).error?.code ?? code;
			} catch {
				// Not JSON: the generic text will do.
			}
			throw new ApiError(res.status, code, '');
		}
		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';
		for (;;) {
			const { done, value } = await reader.read();
			buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
			let cut;
			while ((cut = buffer.indexOf('\n\n')) >= 0) {
				const raw = buffer.slice(0, cut);
				buffer = buffer.slice(cut + 2);
				const event = /^event: (.*)$/m.exec(raw)?.[1];
				const data = JSON.parse(/^data: (.*)$/m.exec(raw)?.[1] ?? '{}');
				if (event === 'delta') assistant.draft.content += data.text ?? '';
				else if (event === 'thinking') assistant.draft.thinking += data.text ?? '';
				else if (event === 'entry') {
					assistant.messages[data.index] = data.entry;
					assistant.messages.length = data.index + 1;
					if (data.entry.role === 'assistant') assistant.draft = { content: '', thinking: '' };
				}
			}
			if (done) break;
		}
	} catch (err) {
		if (err instanceof DOMException && err.name === 'AbortError') {
			// Stopped: keep what came, as the NAS does.
			if (assistant.draft.content || assistant.draft.thinking) {
				assistant.messages.push({ role: 'assistant', content: assistant.draft.content.trim(), thinking: assistant.draft.thinking.trim(), stopped: true });
			}
		} else {
			assistant.messages.push({ role: 'assistant', content: '', problem: { code: problemOf(err) } });
		}
	} finally {
		assistant.draft = { content: '', thinking: '' };
		assistant.busy = false;
		controller = null;
	}
	loadChats();
}

/** Asks the assistant; with edit, replaces the question at that index. */
export async function send(text: string, attachments: ChatAttachment[] = [], edit?: number) {
	const question = text.trim();
	if (!question || assistant.busy) return;
	if (!assistant.chatId) {
		try {
			const chat = await api.post<{ id: string }>('/assistant/chats');
			assistant.chatId = chat.id;
		} catch (err) {
			assistant.error = problemOf(err);
			return;
		}
	}
	const page = currentPage();
	if (edit !== undefined) assistant.messages.length = edit;
	assistant.messages.push({ role: 'user', content: question, attachments, page });
	if (!assistant.title) assistant.title = question.slice(0, 60);
	await stream(`/assistant/chats/${assistant.chatId}/messages`, { text: question, page, model: chosenModel(), attachments, ...(edit !== undefined ? { edit } : {}) });
}

export async function regenerate() {
	if (assistant.busy || !assistant.chatId) return;
	const last = assistant.messages.findLastIndex((m) => m.role === 'user');
	if (last < 0) return;
	assistant.messages.length = last + 1;
	await stream(`/assistant/chats/${assistant.chatId}/regenerate`, { model: chosenModel() });
}

export function stop() {
	controller?.abort();
}

/** Opens a new chat with a question, e.g. from "Ask the assistant" on a message. */
export async function askAbout(question: string, attachments: ChatAttachment[] = []) {
	if (!assistant.status?.available) return;
	assistant.open = true;
	if (assistant.models.length === 0) loadModels();
	stop();
	newChat();
	await send(question, attachments);
}

function short(value: unknown): string {
	if (value === undefined || value === null) return '';
	const text = typeof value === 'string' ? value : JSON.stringify(value);
	return text.length > 1500 ? text.slice(0, 1500) + ' …' : text;
}

/** Runs a confirmed action with the session of this account and tells the assistant. */
export async function runAction(index: number, typed = '') {
	const action = assistant.messages[index]?.action;
	if (!action || action.state !== 'pending' || assistant.busy) return;
	if (action.level === 'destroy' && typed !== action.confirm) return;
	let path = action.path.replace(/^\/api\/v1/, '');
	let body: Record<string, unknown> | undefined = action.body ? { ...action.body } : undefined;
	if (action.level === 'destroy' && action.confirmField) {
		const [where, field] = action.confirmField.split(':');
		if (where === 'query') path += (path.includes('?') ? '&' : '?') + encodeURIComponent(field) + '=' + encodeURIComponent(typed);
		else body = { ...(body ?? {}), [field]: typed };
	}
	let state: 'done' | 'failed' = 'done';
	let result = '';
	assistant.busy = true;
	try {
		const method = action.method.toLowerCase() as 'get' | 'post' | 'put' | 'delete';
		const answer = method === 'get' ? await api.get(path) : await api[method](path, body);
		result = short(answer);
	} catch (err) {
		state = 'failed';
		result = err instanceof ApiError ? `${err.code}: ${err.message}` : String(err);
	}
	assistant.busy = false;
	action.state = state;
	action.result = result;
	await stream(`/assistant/chats/${assistant.chatId}/actions/${action.id}`, { state, result, model: chosenModel() });
}

export async function rejectAction(index: number) {
	const action = assistant.messages[index]?.action;
	if (!action || action.state !== 'pending' || assistant.busy) return;
	action.state = 'rejected';
	await stream(`/assistant/chats/${assistant.chatId}/actions/${action.id}`, { state: 'rejected', result: '', model: chosenModel() });
}

/** Reads a file the person picked as an attachment: text, or an image as base64. */
export async function readAttachment(file: File): Promise<ChatAttachment> {
	if (file.type.startsWith('image/')) {
		if (file.size > 4 << 20) throw new ApiError(413, 'attachment_too_large', '');
		const data = new Uint8Array(await file.arrayBuffer());
		let binary = '';
		for (let i = 0; i < data.length; i += 0x8000) binary += String.fromCharCode(...data.subarray(i, i + 0x8000));
		return { name: file.name, kind: 'image', image: btoa(binary), size: file.size };
	}
	if (file.size > 256 << 10) throw new ApiError(413, 'attachment_too_large', '');
	const text = await file.text();
	if (text.includes('\u0000')) throw new ApiError(415, 'attachment_not_text', '');
	return { name: file.name, kind: 'text', text, size: file.size };
}
