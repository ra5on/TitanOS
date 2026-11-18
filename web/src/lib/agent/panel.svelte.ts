// 1.13 (owner 18 Nov 2026): the chat of the agent's panel in Mail,
// Kontakte and Kalender – one chat through all three programs until
// "Neues Gespräch" (GET/PUT /agent/pim-chat); the agent app lists it like
// every chat (origin "panel"). A small store without navigation: it
// follows a run with follow()/mergeEntries() like the agent app.
import { api } from '../api.ts';
import {
	follow,
	mergeEntries,
	type AgentContext,
	type AgentDraft,
	type AgentEvent,
	type Chat,
	type Entry,
	type Pending,
} from '../agent.ts';
import { apiErrorText } from '../i18n/index.svelte.ts';
import { draftHref } from './drafts.ts';
import { openInProgram } from './open.ts';

export type PanelState = {
	chat: string;
	cloud: boolean;
	cloudNoted: boolean;
	/** Mail tools exist for this account (grant and mail.read). */
	mail: boolean;
};

type Live = { thinking: string; content: string; step: Entry | null };
const idle = (): Live => ({ thinking: '', content: '', step: null });

export class AgentPanelChat {
	state = $state<PanelState | null>(null);
	chat = $state<Chat | null>(null);
	live = $state<Live>(idle());
	busy = $state(false);
	deciding = $state(false);
	sending = $state(false);
	error = $state('');
	#stream: AbortController | null = null;

	/** Reads the panel's chat (once when the panel opens). */
	load = async () => {
		this.error = '';
		try {
			this.state = await api.get<PanelState>('/agent/pim-chat');
			if (this.state.chat) await this.open(this.state.chat);
			else this.chat = null;
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	open = async (id: string) => {
		const chat = await api.get<Chat>(`/agent/chats/${encodeURIComponent(id)}`);
		this.chat = chat;
		if (chat.state === 'running') this.watch(id);
	};

	/** "Neues Gespräch": the next message starts a chat of its own. */
	fresh = async () => {
		this.detach();
		this.chat = null;
		this.live = idle();
		this.busy = false;
		try {
			await api.put('/agent/pim-chat', { chat: '' });
			if (this.state) this.state.chat = '';
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	/** The note about a provider in the internet was seen (Q30). */
	noteCloud = async () => {
		if (this.state) this.state.cloudNoted = true;
		try {
			await api.put('/agent/pim-chat', { cloudNoted: true });
		} catch {
			// Shown again next time; nothing else depends on it.
		}
	};

	send = async (text: string, context?: AgentContext | null): Promise<boolean> => {
		const message = text.trim();
		if (!message || this.sending) return false;
		this.sending = true;
		this.error = '';
		try {
			const body: Record<string, unknown> = { message };
			if (context) body.context = context;
			let chat: Chat;
			if (this.chat) {
				chat = await api.post<Chat>(`/agent/chats/${this.chat.id}/messages`, body);
			} else {
				chat = await api.post<Chat>('/agent/chats', { ...body, origin: 'panel' });
				await api.put('/agent/pim-chat', { chat: chat.id });
				if (this.state) this.state.chat = chat.id;
			}
			this.chat = chat;
			this.watch(chat.id);
			return true;
		} catch (e) {
			this.error = apiErrorText(e);
			return false;
		} finally {
			this.sending = false;
		}
	};

	decide = async (p: Pending, approve: boolean) => {
		if (!this.chat || this.deciding) return;
		const id = this.chat.id;
		this.deciding = true;
		this.error = '';
		try {
			await api.post(`/agent/chats/${id}/decide`, { id: p.id, approve });
			if (this.chat?.id === id) this.chat.pending = null;
			this.watch(id);
		} catch (e) {
			this.error = apiErrorText(e);
			await this.open(id).catch(() => {});
		} finally {
			this.deciding = false;
		}
	};

	/** "Öffnen und bearbeiten" on a waiting step. */
	edit = async (p: Pending) => {
		if (!this.chat || this.deciding) return;
		const id = this.chat.id;
		this.deciding = true;
		this.error = '';
		try {
			const ans = await api.post<{ entry: string; draft: AgentDraft }>(
				`/agent/chats/${id}/decide`,
				{
					id: p.id,
					approve: false,
					edit: true,
				},
			);
			openInProgram(draftHref(ans.draft, id, ans.entry));
			if (this.chat?.id === id) this.chat.pending = null;
			this.watch(id);
		} catch (e) {
			this.error = apiErrorText(e);
		} finally {
			this.deciding = false;
		}
	};

	stop = async () => {
		if (!this.chat) return;
		try {
			await api.post(`/agent/chats/${this.chat.id}/stop`);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	watch = (id: string) => {
		this.detach();
		const controller = new AbortController();
		this.#stream = controller;
		this.busy = true;
		let first = true;
		void follow(
			id,
			(e) => {
				if (first && e.type !== 'snapshot') this.live = idle();
				first = false;
				this.onEvent(id, e);
			},
			controller.signal,
		)
			.catch(() => {})
			.finally(() => {
				if (this.#stream !== controller) return;
				this.#stream = null;
				void this.finish(id);
			});
	};

	onEvent = (id: string, e: AgentEvent) => {
		const chat = this.chat?.id === id ? this.chat : null;
		switch (e.type) {
			case 'snapshot':
				this.live = { thinking: e.thinking ?? '', content: e.content ?? '', step: e.step ?? null };
				if (chat) {
					chat.entries = mergeEntries(chat.entries, e.entries ?? []);
					if (e.state) chat.state = e.state;
					chat.pending = e.pending ?? null;
				}
				break;
			case 'thinking':
				this.live.thinking += e.text;
				break;
			case 'content':
				this.live.content += e.text;
				break;
			case 'step':
				this.live.step = e.step;
				break;
			case 'entry':
				if (chat && !chat.entries.some((x) => x.id === e.entry.id)) chat.entries.push(e.entry);
				if (e.entry.role === 'assistant')
					this.live = { thinking: '', content: '', step: this.live.step };
				if (e.entry.role === 'tool') this.live.step = null;
				break;
			case 'reset':
				this.live = { thinking: '', content: '', step: this.live.step };
				break;
			case 'plan':
				if (chat) chat.plan = e.plan;
				break;
			case 'approval':
				if (chat) {
					chat.pending = e.pending;
					chat.state = 'waiting';
				}
				break;
			case 'state':
				if (chat) chat.state = e.state;
				break;
		}
	};

	finish = async (id: string) => {
		try {
			const fresh = await api.get<Chat>(`/agent/chats/${encodeURIComponent(id)}`);
			if (this.chat?.id === id) this.chat = fresh;
			if (fresh.state === 'running') {
				setTimeout(() => {
					if (this.chat?.id === id && !this.#stream) this.watch(id);
				}, 1500);
				return;
			}
		} catch {
			// The chat stays as it streamed in.
		}
		this.busy = false;
		this.live = idle();
	};

	detach = () => {
		this.#stream?.abort();
		this.#stream = null;
	};
}
