// The chat with the assistant: open or closed, the chats of the account
// and the one that is open. Chats are kept by the NAS (on a volume); the
// browser observes background jobs; only the server executes stored actions.
import { goto } from '$app/navigation';
import { api, ApiError } from '#lib/api.ts';

export type ChatAction = {
	id: string;
	tool: string;
	key: string;
	params?: Record<string, string>;
	reason: string;
	level: 'normal' | 'danger' | 'destroy';
	/** The target name of a destroying action and the field it goes in; the UI sends it back after the confirm dialog. */
	confirm?: string;
	confirmField?: string;
	method: 'GET' | 'POST' | 'PUT' | 'DELETE';
	path: string;
	body?: Record<string, unknown>;
	/** Tools from outside TitanOS the model read before it proposed this. */
	after?: string[];
	state: 'pending' | 'running' | 'uncertain' | 'done' | 'failed' | 'rejected' | 'expired';
	result?: string;
	/** The plan of a task (key "task"). */
	steps?: string[];
	/** Inside an approved task: runs without asking again. */
	auto?: boolean;
};
export type ChatAttachment = { name: string; kind: 'text' | 'image' | 'nas'; text?: string; image?: string; size?: number; root?: string; path?: string };
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
export type AgentRun = { stage?: string; tool?: string; since?: string; state: string; detail?: string; started: string; wake?: string };
export type ModelProfile = { name: string; model: string; prompt: string; numCtx: number; temperature: number | null };
export type ChatSummary = { run?: AgentRun; id: string; title: string; created: string; updated: string; messages: number };
export type AssistantStatus = { available: boolean; enabled: boolean; model: string; web: boolean; persistent: boolean };
export type AssistantModel = { name: string; sizeBytes: number; parameters?: string; quantization?: string; capabilities: string[]; contextLength?: number };
export type AssistantSettings = {
 ocr?:boolean;voiceModel?:string;documentModel?:string;planningModel?:string;
	enabled: boolean;
	execution?: 'confirm' | 'direct';
	paused?: boolean;
	systemPrompt?: string;
	promptVersion?: string;
	promptHistory?: string[];
	profiles?: ModelProfile[];
	memory?: string;
	retentionDays?: number;
	ephemeral?: boolean;
	pushApproval?: boolean;
	/** Empty or ollama is Ollama; anthropic and openai send chats to that provider. */
	provider?: '' | 'ollama' | 'anthropic' | 'openai';
	openaiBase?: string;
	/** Keys only go to the NAS; it answers with whether one is set. */
	anthropicKey?: string;
	openaiKey?: string;
	clearAnthropicKey?: boolean;
	clearOpenaiKey?: boolean;
	anthropicKeySet?: boolean;
	openaiKeySet?: boolean;
	ollama: string;
	model: string;
	users: boolean;
	web: boolean;
	webUsers: boolean;
	volume: string;
	prompt: string;
	numCtx: number;
	temperature: number | null;
	maxSteps: number;
	areas: Record<string, 'off' | 'read' | 'write'>;
};
export type AssistantPrefs = { floating: boolean; pageContext: boolean; model: string };

const DEFAULT_PREFS: AssistantPrefs = { floating: true, pageContext: true, model: '' };

/** Areas of the NAS, in the order of the settings page. */
export const AREAS = ['files', 'docker', 'vms', 'storage', 'shares', 'backups', 'accounts', 'system', 'updates', 'home'] as const;

/** Statistics of a chat: rounds with the model, tokens, time. */
export type RunStats = { rounds: number; prompt: number; generated: number; modelMs: number; toolMs: number; tools: number };

export const assistant = $state({
	/** The page the chat was opened from, sent along as context. */
	fromPage: '',
	/** null until read: whether this account may chat at all. */
	status: null as AssistantStatus | null,
	models: [] as AssistantModel[],
	chats: [] as ChatSummary[],
	search: '',
	chatId: '',
	title: '',
	messages: [] as ChatEntry[],
	busy: false,
	run: null as AgentRun | null,
	/** What the work of the chat cost, as the NAS adds it up. */
	stats: null as RunStats | null,
	dryRun: false,
	archived: 0,
	/** The answer that is being written right now. */
	draft: { content: '', thinking: '' },
	error: '',
	prefs: { ...DEFAULT_PREFS } as AssistantPrefs
});

let controller: AbortController | null = null;
let viewGeneration = 0;
function detach() { viewGeneration++; controller?.abort(); controller=null; assistant.busy=false; assistant.draft={content:'',thinking:''}; }

export function useAssistantPrefs(value: Record<string, unknown> | null) {
	const v = value ?? {};
	assistant.prefs = {
		floating: typeof v.floating === 'boolean' ? v.floating : DEFAULT_PREFS.floating,
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
	const account = accountGeneration;
	try {
		const status = await api.get<AssistantStatus>('/assistant/status');
		if (account === accountGeneration) assistant.status = status;
	} catch {
		if (account === accountGeneration) assistant.status = { available: false, enabled: false, model: '', web: false, persistent: false };
	}
}

export async function loadModels() {
	const account = accountGeneration;
	try {
		const models = (await api.get<{ models: AssistantModel[] }>('/assistant/models')).models;
		if (account === accountGeneration) assistant.models = models;
	} catch {
		if (account === accountGeneration) assistant.models = [];
	}
}

/** The model of this account, or the default of the NAS. */
export function chosenModel(): string {
	return assistant.prefs.model || assistant.status?.model || '';
}

export async function loadChats() {
	const account = accountGeneration;
	try {
		const query = assistant.search.trim() ? `?search=${encodeURIComponent(assistant.search.trim())}` : '';
		const res = await api.get<{ chats: ChatSummary[]; persistent: boolean }>('/assistant/chats' + query);
		if (account !== accountGeneration) return;
		assistant.chats = res.chats;
		if (assistant.status) assistant.status.persistent = res.persistent;
		assistant.error = '';
	} catch (err) {
		if (account === accountGeneration) assistant.error = problemOf(err);
	}
}

/** Opens the chat page, remembering where it was opened from. */
export function openAssistant() {
	if (!assistant.status?.available || typeof location === 'undefined') return;
	if (location.pathname !== '/assistant') {
		assistant.fromPage = location.pathname + location.search;
		goto('/assistant');
	}
}

/** The same, for the button in the header, the round button and Ctrl+J. */
export function toggleAssistant() {
	openAssistant();
}

export async function openChat(id: string) {
 detach(); const view=viewGeneration;
	try {
		const res = await api.get<{ chat: { id: string; title: string; messages: ChatEntry[]; run?: AgentRun; dryRun?: boolean; archived?: number; stats?: RunStats }; busy: boolean }>(`/assistant/chats/${id}`);
 if(view!==viewGeneration) return;
		assistant.chatId = res.chat.id;
		assistant.title = res.chat.title;
		assistant.messages = res.chat.messages;
		assistant.run = res.chat.run ?? null;
		assistant.stats = res.chat.stats ?? null;
		assistant.busy = res.busy;
		assistant.dryRun = res.chat.dryRun ?? false;
		assistant.archived = res.chat.archived ?? 0;
		watchChat();
		assistant.error = '';
	} catch (err) {
		assistant.error = problemOf(err);
	}
}

// Bumped when the signed-in account changes, so late answers for the
// previous account are dropped.
let accountGeneration = 0;

/** Forgets everything of the signed-in account: chats, the open chat, timers. Called on logout and account change. */
export function resetAssistant() {
	accountGeneration++;
	newChat();
	clearTimeout(poll);
	poll = undefined;
	clearTimeout(saveTimer);
	saveTimer = undefined;
	assistant.fromPage = '';
	assistant.status = null;
	assistant.models = [];
	assistant.chats = [];
	assistant.search = '';
	assistant.dryRun = false;
	assistant.prefs = { ...DEFAULT_PREFS };
}

/** Starts an empty chat; the NAS creates it with the first message. */
export function newChat() {
 detach();
	assistant.chatId = '';
	assistant.run = null;
	assistant.stats = null;
	assistant.archived = 0;
	assistant.title = '';
	assistant.messages = [];
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
	if (!assistant.prefs.pageContext) return '';
	const page = assistant.fromPage;
	return /^\/[A-Za-z0-9/_.~?=&%+-]{0,199}$/.test(page) && !page.startsWith('/assistant') ? page : '';
}

/** Sends to a route of the chat and follows what comes back. */
async function stream(path: string, body: Record<string, unknown>) {
	assistant.busy = true;
	assistant.draft = { content: '', thinking: '' };
	assistant.error = '';
	const active = new AbortController();
 controller=active; const view=viewGeneration;
	try {
		const res = await fetch('/api/v1' + path, {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(body),
			signal: active.signal
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
			if(controller!==active || view!==viewGeneration) return;
			buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
			let cut;
			while ((cut = buffer.indexOf('\n\n')) >= 0) {
				const raw = buffer.slice(0, cut);
				buffer = buffer.slice(cut + 2);
				const event = /^event: (.*)$/m.exec(raw)?.[1];
				const data = JSON.parse(/^data: (.*)$/m.exec(raw)?.[1] ?? '{}');
				if (event === 'snapshot') { assistant.messages = data.chat.messages; assistant.run = data.chat.run; assistant.stats = data.chat.stats ?? null; assistant.archived = data.chat.archived ?? 0; }
				else if (event === 'progress') assistant.run = data;
                else if (event === 'problem') assistant.error = data.code;
				else if (event === 'delta') assistant.draft.content += data.text ?? '';
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
 if(controller!==active || view!==viewGeneration) return;
		if (err instanceof DOMException && err.name === 'AbortError') {
			// Stopped: keep what came, as the NAS does.
			if (assistant.draft.content || assistant.draft.thinking) {
				assistant.messages.push({ role: 'assistant', content: assistant.draft.content.trim(), thinking: assistant.draft.thinking.trim(), stopped: true });
			}
		} else {
			assistant.messages.push({ role: 'assistant', content: '', problem: { code: problemOf(err) } });
		}
	} finally {
 if(controller===active && view===viewGeneration) {
		assistant.draft = { content: '', thinking: '' };
		assistant.busy = false;
		controller = null;
 }
	}
 if(view!==viewGeneration) return;
	loadChats();
	await refreshChat();
	watchChat();
}

/** Asks the assistant; with edit, replaces the question at that index. */
export async function send(text: string, attachments: ChatAttachment[] = [], edit?: number) {
	const question = text.trim();
	if (!question) return;
	if (assistant.busy) { detach(); await stream(`/assistant/chats/${assistant.chatId}/control`, {action: 'steer', text: question}); return; }
	if (!assistant.chatId) {
		// Busy already, so that the page does not open another chat meanwhile.
		assistant.busy = true;
		try {
			const chat = await api.post<{ id: string }>('/assistant/chats');
			assistant.chatId = chat.id;
		} catch (err) {
			assistant.error = problemOf(err);
			assistant.busy = false;
			return;
		} finally {
			assistant.busy = false;
		}
	}
	const page = currentPage();
	if (edit !== undefined) assistant.messages.length = edit;
	assistant.messages.push({ role: 'user', content: question, attachments, page });
	if (!assistant.title) assistant.title = question.slice(0, 60);
	await stream(`/assistant/chats/${assistant.chatId}/messages`, { dryRun: assistant.dryRun, text: question, page, model: assistant.prefs.model, attachments, ...(edit !== undefined ? { edit } : {}) });
}

export async function regenerate() {
	if (assistant.busy || !assistant.chatId) return;
	const last = assistant.messages.findLastIndex((m) => m.role === 'user');
	if (last < 0) return;
	assistant.messages.length = last + 1;
	await stream(`/assistant/chats/${assistant.chatId}/regenerate`, { model: chosenModel() });
}

export function stop() {
 detach();
	if (assistant.chatId) api.post(`/assistant/chats/${assistant.chatId}/control`, {action:'stop'}).then(refreshChat).catch(err => assistant.error = problemOf(err));
}
export async function controlRun(action: 'pause' | 'resume' | 'reconcile') {
 detach();
 try {
	if (action === 'resume' || action === 'reconcile') await stream(`/assistant/chats/${assistant.chatId}/control`, {action});
	else { await api.post(`/assistant/chats/${assistant.chatId}/control`, {action}); await refreshChat(); }
 } catch(err) {assistant.error=problemOf(err);}
}
let poll: ReturnType<typeof setTimeout> | undefined;
export async function refreshChat() {
	const id=assistant.chatId; if (!id) return;
	try {
		const res=await api.get<{chat:{messages:ChatEntry[];run?:AgentRun;archived?:number;stats?:RunStats};busy:boolean}>(`/assistant/chats/${id}`);
		if (id!==assistant.chatId) return;
		assistant.messages=res.chat.messages; assistant.run=res.chat.run??null; assistant.stats=res.chat.stats??null; assistant.archived=res.chat.archived??0;
		if (!controller) assistant.busy=res.busy;
	} catch(err) {assistant.error=problemOf(err);}
}
function watchChat() {
	clearTimeout(poll);
	if (typeof window === 'undefined' || !assistant.chatId) return;
	poll=setTimeout(async()=>{if (!controller) await refreshChat(); watchChat();},3000);
}

/** Opens a new chat with a question, e.g. from "Ask the assistant" on a message. */
export async function askAbout(question: string, attachments: ChatAttachment[] = []) {
	if (!assistant.status?.available) return;
	if (assistant.models.length === 0) loadModels();
	detach();
	openAssistant();
	newChat();
	await send(question, attachments);
}

/** Approval is bound to a stored action. The NAS executes and records it. */
export async function runAction(index: number, typed = '') {
	const action=assistant.messages[index]?.action;
	if (!action || action.state!=='pending' || assistant.busy) return;
	await stream(`/assistant/chats/${assistant.chatId}/actions/${action.id}`, {state:'approved',typed,confirmed:true,model:chosenModel()});
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
