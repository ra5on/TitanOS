<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { page } from '$app/state';
	import { personalMenu } from './menu.ts';

	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { session } from '#lib/session.svelte.ts';
	import { poll, type Poller } from '#lib/poll.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { openInProgram } from '#lib/agent/open.ts';
	import {
		agentState,
		createJob,
		changeJob,
		saveSettings,
		requestId,
		searchAgent,
		renameChat,
		deleteChat,
		type SearchResult,
		type Action,
		type Job,
		type State,
		type Settings,
		type Source,
	} from './api.ts';
	import { consumeSelection, nativeDraftLink } from './open.ts';
	import {
		jobOutput,
		stepTool,
		jobProgressChanged,
		visibleSources,
		mailFields,
		sourceLocation,
	} from './sources.ts';
	import SourcePreview from './SourcePreview.svelte';
	import Attachments from './Attachments.svelte';
	import { colorStyle } from './appearance.ts';
	import { motifFor, depart, type Cue } from './guide.ts';
	import { localReadiness, modelStatus, usesLocalModel, type LocalModel } from './model.ts';
	import { msg, sourceTitle, iconFor } from './labels.ts';
	import MotionGuide from './MotionGuide.svelte';
	import JobDetail from './JobDetail.svelte';
	import Jobs from './Jobs.svelte';
	import Sheet from './Sheet.svelte';
	import VoiceInput from './VoiceInput.svelte';
	import ChatView from './ChatView.svelte';
	import { startPresence } from './presence.ts';
	import SettingsView from './SettingsView.svelte';
	import OfficeTools from './OfficeTools.svelte';
	import './personal.css';

	type Tab = 'chat' | 'jobs' | 'settings';
	const tabs: { id: Tab; icon: IconName }[] = [
		{ id: 'chat', icon: 'chat' },
		{ id: 'jobs', icon: 'list' },
		{ id: 'settings', icon: 'gear' },
	];
	let snapshot = $state<State | null>(null),
		error = $state(''),
		tab = $state<Tab>('chat'),
		chatId = $state(''),
		detailId = $state(''),
		firstId = $state(''),
		search = $state('');
	let openedSource = $state<Source | null>(null);
	// Retain the three-dot phase while the user switches views in this workspace.
	const phases = new Map<string, { seconds: number }>();
	function phaseFor(id: string) {
		let phase = phases.get(id);
		if (!phase) {
			phase = { seconds: 0 };
			phases.set(id, phase);
		}
		return phase;
	}

	let sheet = $state<'' | 'history' | 'context' | 'voice'>(''),
		sources = $state<Source[]>([]),
		tool = $state('chat.ask'),
		prompt = $state('');
	let args = $state<Record<string, unknown>>({}),
		valid = $state(true),
		pending = $state(false),
		draft = $state<Partial<Settings> | null>(null),
		cue = $state<Cue | null>(null);
	let box = $state<HTMLElement>(),
		scroller = $state<HTMLElement>(),
		input = $state<HTMLTextAreaElement>(),
		width = $state(0),
		reduced = $state(false);
	let polling: Poller | undefined,
		sequence = 0,
		previousSubmission = '',
		nonce = '',
		lastContext = '',
		linkGeneration = 0,
		visited = new Set<string>(),
		initialized = false;
	const narrow = $derived(width < 1040),
		small = $derived(width < 710);
	const chosen = $derived(snapshot?.chats.find((c) => c.id === chatId));
	// 1.38 (§124): rename and delete a chat from its row in the list.
	let chatTools = $state(''),
		chatName = $state(''),
		chatDelete = $state(false),
		chatBusy = $state(false),
		chatError = $state('');
	function toggleChatTools(id: string, title: string) {
		chatTools = chatTools === id ? '' : id;
		chatName = title;
		chatDelete = false;
		chatError = '';
	}
	async function chatAction(work: () => Promise<unknown>, after?: () => void) {
		chatBusy = true;
		chatError = '';
		try {
			await work();
			after?.();
			chatTools = '';
			setData(await agentState());
		} catch (e) {
			chatError = apiErrorText(e);
		} finally {
			chatBusy = false;
		}
	}
	const rows = $derived(snapshot?.jobs.filter((j) => j.request.chatId === chatId) ?? []);
	const detail = $derived(snapshot?.jobs.find((j) => j.id === detailId));
	const counts = $derived(snapshot?.jobs.filter((j) => j.status === 'waiting').length ?? 0);
	const appearance = $derived(snapshot ? { ...snapshot.settings, ...draft } : undefined);
	const calm = $derived(reduced || appearance?.motion === 'off');
	let result = $state<SearchResult | null>(null);
	let searchError = $state('');
	$effect(() => {
		const query = search.trim(),
			account = session.data?.user?.name;
		result = null;
		searchError = '';
		if (!query || !account) return;
		const controller = new AbortController();
		const timer = setTimeout(() => {
			void searchAgent(query, controller.signal)
				.then((r) => {
					if (!controller.signal.aborted) result = r;
				})
				.catch((e) => {
					if (!controller.signal.aborted) searchError = apiErrorText(e);
				});
		}, 180);
		return () => {
			clearTimeout(timer);
			controller.abort();
		};
	});
	const filteredChats = $derived(
		search.trim() ? (result?.chats ?? []) : (snapshot?.chats ?? []).toReversed(),
	);
	const options = $derived((snapshot?.capabilities ?? []).filter((c) => c.effective !== 'never'));
	function matches(tool: string, selected: Source[]) {
		if (!selected.length) return ['chat.ask', 'chat.cloud', 'backup.status'].includes(tool);
		if (selected.every((s) => s.kind === 'office.region')) {
			const ref = selected[0].ref;
			return (
				tool === 'office.inspect' ||
				(tool === 'office.copy' && selected.length === 1 && !(ref?.slide && !ref.paragraph))
			);
		}
		if (selected.every((s) => s.kind === 'file')) {
			if (tool === 'files.read') return true;
			return (
				selected.length === 1 &&
				/\.(docx|xlsx|pdf|pptx)$/i.test(selected[0].id) &&
				['office.copy', 'office.inspect'].includes(tool)
			);
		}
		const files = selected.filter((s) => s.kind === 'file');
		if (
			selected.length === files.length + 1 &&
			files.length >= 2 &&
			files.length <= 8 &&
			selected.some((s) => s.kind === 'folder')
		) {
			return (
				(tool === 'office.pdf-merge' && files.every((s) => /\.pdf$/i.test(s.id))) ||
				(tool === 'office.compare' &&
					files.every((s) => /\.(pdf|docx|xlsx|txt|md|tsv|csv)$/i.test(s.id)))
			);
		}
		if (selected.length !== 1) return false;
		if (selected[0].kind === 'folder')
			return ['files.search', 'office.table', 'office.document', 'office.deck'].includes(tool);
		return selected[0].kind === 'attachment' && tool === 'attachment.read';
	}
	let model = $state<LocalModel>({ state: 'checking' }),
		modelRefresh = $state(0),
		settingsFocus = $state<'' | 'model' | 'permissions'>('');
	const localAllowed = $derived(options.some((c) => usesLocalModel(c.id)));
	$effect(() => {
		const owner = session.data?.user?.name;
		void modelRefresh;
		model = { state: 'checking' };
		if (!owner || !localAllowed) return;
		return poll(
			async (signal) => {
				const next = await localReadiness(signal);
				if (!signal.aborted && session.data?.user?.name === owner) model = next;
			},
			{
				interval: 60000,
				onError: () => {
					model = { state: 'unavailable' };
				},
			},
		);
	});
	const readyOptions = $derived(
		options.filter((c) => !usesLocalModel(c.id) || model.state === 'ready'),
	);
	const tools = $derived(readyOptions.filter((c) => matches(c.id, sources)));
	const canSubmit = $derived(tools.some((c) => c.id === tool));
	function syncTool() {
		if (
			(tool === 'chat.ask' && !sources.length) ||
			options.some((c) => c.id === tool && matches(tool, sources))
		)
			return;
		const standard = sources.length
			? (options.find((c) => matches(c.id, sources))?.id ?? '')
			: 'chat.ask';
		setTool(standard);
	}
	function addSource(source: Source) {
		if (
			!sources.some(
				(s) => s.kind === source.kind && s.id === source.id && s.ref?.root === source.ref?.root,
			)
		)
			sources = [...sources, source];
		syncTool();
	}
	function removeSource(index: number) {
		sources = sources.filter((_, i) => i !== index);
		syncTool();
	}
	const selected = $derived(snapshot?.capabilities.find((c) => c.id === tool));
	const office = $derived(tool.startsWith('office.'));
	personalMenu({
		ready: () => !!snapshot,
		modal: () => !!sheet || !!openedSource || (!!detail && narrow),
		chats: () => snapshot?.chats ?? [],
		selected: () => chatId,
		tab: () => tab,
		show,
		newChat,
		openChat,
	});
	function signal(stops: string[], action: string, hold = false) {
		const origin = box?.getBoundingClientRect();
		const first = [...(box?.querySelectorAll<HTMLElement>('[data-guide]') ?? [])]
			.find((n) => n.dataset.guide === stops[0] && n.getClientRects().length)
			?.getBoundingClientRect();
		const start =
			first && origin
				? {
						x: first.left + first.width / 2 - origin.left,
						y: first.top + first.height / 2 - origin.top,
					}
				: undefined;
		cue = { id: ++sequence, stops, motif: motifFor(action), hold, origin: start };
	}
	function clearContext() {
		sources = [];
		prompt = '';
		args = {};
		valid = true;
		tool = 'chat.ask';
		nonce = '';
		previousSubmission = '';
	}
	function show(t: Tab, focus: '' | 'model' | 'permissions' = '') {
		cue = null;
		settingsFocus = focus;
		tab = t;
		sheet = '';
		detailId = '';
		firstId = '';
		draft = null;
	}
	function newChat() {
		show('chat');
		chatId = '';
		clearContext();
		void tick().then(() => input?.focus());
	}
	function openChat(id: string) {
		show('chat');
		clearContext();
		chatId = id;
		void scrollBottom();
	}
	async function scrollBottom() {
		await tick();
		if (scroller) scroller.scrollTop = scroller.scrollHeight;
	}
	async function seen(j: Job) {
		if (j.status !== 'done' || !j.result || j.seen || visited.has(j.id)) return;
		visited.add(j.id);
		firstId = j.id;
		try {
			await changeJob(j.id, 'seen');
		} catch {
			visited.delete(j.id);
		}
	}
	function select(j: Job) {
		detailId = j.id;
		firstId = '';
		sheet = '';
		void seen(j);
		signal(
			[
				'card-' + j.id,
				...(jobOutput(j)?.sources ?? []).map((_, i) => j.id + '-source-' + i),
				'detail',
			],
			stepTool(j),
		);
	}
	function setData(next: State) {
		next = {
			...next,
			jobs: next.jobs ?? [],
			chats: next.chats ?? [],
			capabilities: next.capabilities ?? [],
		};
		const previous = snapshot;
		snapshot = next;
		if (!initialized) {
			initialized = true;
			tool = 'chat.ask';
		}
		if (previous) {
			const j = next.jobs.findLast(
				(j) =>
					(j.id === detailId || (tab === 'chat' && j.request.chatId === chatId)) &&
					jobProgressChanged(
						previous.jobs.find((v) => v.id === j.id),
						j,
					),
			);
			if (j?.status === 'done' || j?.status === 'waiting') {
				signal(
					['reply-' + j.id, 'card-' + j.id, ...(detailId === j.id ? ['detail'] : [])],
					stepTool(j),
				);
				if (detailId === j.id) void seen(j);
			} else if (j?.status === 'running') {
				signal(
					[
						'reply-' + j.id,
						...visibleSources(j).map((_, i) => j.id + '-source-' + i),
						detailId === j.id ? 'detail' : 'step-' + j.id,
					],
					stepTool(j),
				);
			} else if (j?.status === 'paused') cue = null;
		}
	}
	$effect(() => {
		const account = session.data?.user?.name;
		if (!account) return;
		initialized = false;
		snapshot = null;
		chatId = '';
		detailId = '';
		sources = [];
		prompt = '';
		visited = new Set();
		phases.clear();
		cue = null;
		draft = null;
		lastContext = '';
		const p = poll(
			async (abort) => {
				const next = await agentState();
				if (!abort.aborted && session.data?.user?.name === account) setData(next);
			},
			{
				interval: () =>
					snapshot?.jobs.some((j) => j.status === 'running' || j.status === 'planned')
						? 1800
						: 6000,
				onError: (e) => (error = apiErrorText(e)),
			},
		);
		polling = p;
		return () => {
			p();
			polling = undefined;
			snapshot = null;
			sources = [];
			prompt = '';
			linkGeneration++;
			openedSource = null;
			search = '';
			error = '';
			sheet = '';
		};
	});
	$effect(() => {
		const current = page.url.searchParams,
			context = current.get('context'),
			job = current.get('job'),
			chat = current.get('chat');
		if (!snapshot) return;
		untrack(() => {
			if (context && context !== lastContext) {
				lastContext = context;
				const gen = ++linkGeneration;
				void consumeSelection(context)
					.then((h) => {
						if (gen !== linkGeneration) return;
						show('chat');
						chatId = '';
						sources = h.sources ?? [];
						tool =
							h.tool ??
							(sources.length
								? sources[0].kind === 'folder'
									? 'office.table'
									: 'files.read'
								: 'chat.ask');
						prompt = h.prompt ?? '';
						args = {};
						valid = !tool.startsWith('office.');
						if (!matches(tool, sources)) syncTool();
						sheet = 'context';
					})
					.catch((e) => {
						if (gen === linkGeneration) error = apiErrorText(e);
					});
				return;
			}
			if (job && lastContext !== job) {
				const j = snapshot!.jobs.find((v) => v.id === job);
				if (j) {
					lastContext = job;
					chatId = j.request.chatId ?? '';
					select(j);
				}
			} else if (chat && lastContext !== chat && snapshot!.chats.some((c) => c.id === chat)) {
				lastContext = chat;
				openChat(chat);
			}
		});
	});
	// 1.39: no push while this window is in view (presence.ts).
	onMount(() => startPresence());
	onMount(() => {
		const media = matchMedia('(prefers-reduced-motion: reduce)');
		const update = () => {
			reduced = media.matches;
		};
		update();
		media.addEventListener('change', update);
		const observer = new ResizeObserver((entries) => {
			width = entries[0]?.contentRect.width ?? 0;
		});
		if (box) observer.observe(box);
		return () => {
			observer.disconnect();
			media.removeEventListener('change', update);
		};
	});
	async function save(s: Partial<Settings>) {
		const owner = session.data?.user?.name;
		pending = true;
		error = '';
		try {
			const next = await saveSettings(s);
			if (session.data?.user?.name === owner) setData(next);
		} catch (e) {
			error = apiErrorText(e);
			throw e;
		} finally {
			pending = false;
		}
	}
	async function act(action: Action, j: Job) {
		if (pending) return;
		pending = true;
		error = '';
		try {
			await changeJob(j.id, action, action === 'approve' ? j.token : undefined);
			await polling?.now();
		} catch (e) {
			error = apiErrorText(e);
			await polling?.now();
		} finally {
			pending = false;
		}
	}
	function pages(index: number, first: number, last: number) {
		if (!Number.isFinite(first) || !Number.isFinite(last)) return;
		const from = Math.max(1, Math.min(10000, Math.trunc(first)));
		const to = Math.max(from, Math.min(10000, from + 24, Math.trunc(last)));
		sources = sources.map((s, i) =>
			i === index ? { ...s, ref: { first: String(from), last: String(to) } } : s,
		);
	}
	function officeChange(v: Record<string, unknown>, ok: boolean) {
		args = v;
		valid = ok;
	}
	function sourceOpen(src: Source, job: Job) {
		const dest = sourceLocation(src);
		if (!dest) return;
		signal(
			[
				'detail',
				'evidence-' +
					Math.max(
						0,
						(jobOutput(job)?.sources ?? job.request.sources ?? []).findIndex(
							(s) => s.id === src.id,
						),
					),
			],
			stepTool(job),
		);
		if (dest.pdf) {
			openedSource = src;
			return;
		}
		openInProgram(dest.href);
	}
	async function editDraft(j: Job) {
		const href = nativeDraftLink(j);
		if (href) {
			signal(['card-' + j.id, 'editor-' + j.id], stepTool(j));
			await depart(box);
			if (box?.isConnected) openInProgram(href);
		}
	}
	async function reviewDraft(j: Job) {
		const mail = mailFields(j),
			src = (j.result?.sources ?? j.request.sources ?? []).find((s) => s.kind === 'mail');
		if (!mail?.isDraft || !mail.text || !src || pending) return;
		if (!options.some((c) => c.id === 'mail.send')) {
			show('settings');
			return;
		}
		// An already-created send is the same conversation action on all devices.
		const request = 'draft-send-' + j.id;
		const existing = snapshot?.jobs.find((item) => item.request.requestId === request);
		const owner = session.data?.user?.name;
		if (existing) {
			select(existing);
			return;
		}
		pending = true;
		error = '';
		try {
			const send = await createJob({
				tool: 'mail.send',
				chatId: j.request.chatId,
				requestId: request,
				sources: [src],
				args: { text: mail.text },
			});
			if (session.data?.user?.name !== owner) return;
			allowJob(send);
			select(send);
			await polling?.now();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			pending = false;
		}
	}
	function setTool(id: string) {
		tool = id;
		args = {};
		valid = !id.startsWith('office.');
	}
	async function submit(example = false) {
		const id = example ? 'agent.example' : tool,
			text = example ? '' : prompt.trim();
		if (
			pending ||
			!options.some((c) => c.id === id) ||
			(!example && !canSubmit) ||
			(!example && office && !valid) ||
			(!text && (id.startsWith('chat.') || id === 'files.search'))
		)
			return;
		const body = {
			tool: id,
			chatId: chatId || undefined,
			prompt: text || undefined,
			sources: example ? [] : sources,
			args: example ? {} : args,
		};
		const key = JSON.stringify(body);
		if (key !== previousSubmission || !nonce) {
			previousSubmission = key;
			nonce = requestId();
		}
		const owner = session.data?.user?.name;
		pending = true;
		error = '';
		try {
			const j = await createJob({ ...body, requestId: nonce });
			if (session.data?.user?.name !== owner) return;
			chatId = j.request.chatId ?? '';
			allowJob(j);
			prompt = '';
			nonce = '';
			sheet = '';
			signal(['composer', ...sources.map((_, i) => 'source-' + i), 'reply-' + j.id], id);
			await polling?.now();
			await scrollBottom();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			pending = false;
		}
	}
	async function voiceTurn(text: string, id: string): Promise<Job> {
		const account = session.data?.user?.name;
		const previousChat = chatId;
		const j = await createJob({
			tool: 'chat.ask',
			prompt: text,
			requestId: id,
			chatId: previousChat || undefined,
		});
		if (!account || session.data?.user?.name !== account) throw Error(msg('voiceCheckJob'));
		allowJob(j);
		if (chatId === previousChat) chatId = j.request.chatId ?? previousChat;
		void polling?.now();
		void scrollBottom();
		return j;
	}
	function allowJob(j: Job) {
		if (snapshot && !snapshot.jobs.some((item) => item.id === j.id))
			snapshot = { ...snapshot, jobs: [...snapshot.jobs, j] };
	}
	function fromSkill(j: Job) {
		show('jobs');
		allowJob(j);
		select(j);
		void polling?.now();
	}
	function background(j: Job) {
		show('jobs');
		signal(['reply-' + j.id, 'jobs-tab'], stepTool(j));
	}
	function onkey(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && (!small || e.metaKey || e.ctrlKey)) {
			e.preventDefault();
			void submit();
		}
	}
	function closeDetail() {
		detailId = '';
		firstId = '';
	}
</script>

<div
	class="pa-ui pa-shell"
	class:pa-small={small}
	class:pa-narrow={narrow}
	class:pa-expanded={!!detail && !narrow}
	bind:this={box}
	data-theme-choice={appearance?.theme ?? 'system'}
	data-motion={calm ? 'off' : appearance?.motion}
	style={colorStyle(appearance)}
>
	{#snippet navButtons()}
		{#each tabs as t}<button
				aria-current={tab === t.id ? 'page' : undefined}
				data-guide={t.id + '-tab'}
				onclick={() => show(t.id)}
				><Icon name={t.icon} /><span>{msg(t.id)}</span>{#if t.id === 'jobs' && counts}<small
						class="pa-count"
						aria-label={counts + ' ' + msg('attention')}>{counts}</small
					>{/if}</button
			>{/each}
	{/snippet}
	{#snippet history()}
		{#if (snapshot?.chats.length ?? 0) > 0}<input
				aria-label={msg('search')}
				type="search"
				placeholder={msg('search')}
				bind:value={search}
			/>{/if}
		<div class="pa-history">
			{#each filteredChats as chat (chat.id)}<div class="pa-chat-row">
					<button
						aria-current={chat.id === chatId ? 'page' : undefined}
						onclick={() => openChat(chat.id)}>{chat.title}</button
					><button
						class="pa-icon pa-chat-more"
						aria-label={msg('chatMenu') + ': ' + chat.title}
						aria-expanded={chatTools === chat.id}
						onclick={() => toggleChatTools(chat.id, chat.title)}
						><Icon name="more" size={16} /></button
					>
				</div>
				{#if chatTools === chat.id}<div class="pa-approval pa-chat-tools">
						{#if chatDelete}<strong>{msg('chatDeleteTitle')}</strong>
							<p class="pa-muted">{msg('chatDeleteText')}</p>
							<div class="pa-actions">
								<button
									class="pa-button pa-danger"
									disabled={chatBusy}
									onclick={() =>
										chatAction(
											() => deleteChat(chat.id),
											() => {
												if (chatId === chat.id) chatId = '';
											},
										)}>{msg('chatDelete')}</button
								><button class="pa-button" onclick={() => (chatDelete = false)}
									>{msg('cancel')}</button
								>
							</div>
						{:else}<form
								onsubmit={(e) => {
									e.preventDefault();
									if (chatName.trim()) void chatAction(() => renameChat(chat.id, chatName));
								}}
							>
								<label>{msg('chatName')}<input bind:value={chatName} maxlength="120" /></label>
								<div class="pa-actions">
									<button class="pa-button" type="submit" disabled={chatBusy || !chatName.trim()}
										>{msg('save')}</button
									><button
										class="pa-button pa-danger"
										type="button"
										onclick={() => (chatDelete = true)}>{msg('chatDelete')}</button
									>
								</div>
							</form>{/if}
						{#if chatError}<small role="alert">{chatError}</small>{/if}
					</div>{/if}{/each}
			{#each result?.jobs ?? [] as hit (hit.id)}<button
					onclick={() => {
						const job = snapshot?.jobs.find((j) => j.id === hit.id);
						if (job) {
							openChat(hit.chatId);
							select(job);
						}
					}}><Icon name="list" size={14} />{hit.title}</button
				>{/each}
			{#if searchError}<small role="alert">{searchError}</small
				>{:else if !filteredChats.length && (!search || result) && !result?.jobs?.length}<small
					class="pa-muted">{msg(search ? 'noMatches' : 'emptyChats')}</small
				>{/if}
		</div>
	{/snippet}
	{#snippet brand()}<span class="pa-logo"><Icon name="sparkle" size={21} /></span>
		<div>
			<strong>Agent</strong><small
				>{small ? session.data?.user?.name + ' · ' + msg('onlyRights') : msg('assistant')}</small
			>
		</div>{/snippet}
	{#snippet context()}
		<div class="pa-context">
			<h3>{msg('available')}</h3>
			{#if tools.length}<label class="pa-field"
					>{msg('tool')}<select
						value={canSubmit ? tool : ''}
						onchange={(e) => setTool(e.currentTarget.value)}
						>{#if !canSubmit}<option value="" disabled>{msg('chooseTool')}</option
							>{/if}{#each tools as c (c.id)}<option value={c.id}>{c.title}</option>{/each}</select
					></label
				>{/if}
			<Attachments
				{sources}
				allowed={options.some((c) => c.id === 'attachment.read')}
				filesAllowed={options.some((c) => c.id.startsWith('files.') || c.id.startsWith('office.'))}
				onselect={addSource}
			/>
			{#if office}<OfficeTools {tool} {sources} onchange={officeChange} />{/if}
			{#if sources.length}<h3>{msg('chatContext')}</h3>
				<div class="pa-chips">
					{#each sources as src, i}<button
							aria-label={msg('remove') + ' ' + sourceTitle(src)}
							onclick={() => removeSource(i)}
							><Icon name={iconFor(src.kind)} size={14} />{sourceTitle(src)}<Icon
								name="close"
								size={14}
							/></button
						>{/each}
				</div>{:else}<p class="pa-muted">{msg('sourceHelp')}</p>{/if}
			{#each sources as src, i}{#if src.kind === 'attachment'}<fieldset class="pa-formgrid">
						<legend>{sourceTitle(src)} · {msg('pdfPages')}</legend>
						<label
							>{msg('firstPage')}<input
								type="number"
								min="1"
								max="10000"
								value={src.ref?.first ?? 1}
								onchange={(e) =>
									pages(i, e.currentTarget.valueAsNumber, Number(src.ref?.last || 5))}
							/></label
						>
						<label
							>{msg('lastPage')}<input
								type="number"
								min={src.ref?.first ?? 1}
								max={Math.min(10000, Number(src.ref?.first || 1) + 24)}
								value={src.ref?.last ?? 5}
								onchange={(e) =>
									pages(i, Number(src.ref?.first || 1), e.currentTarget.valueAsNumber)}
							/></label
						>
					</fieldset>{/if}{/each}
			<button
				class="pa-button"
				onclick={() => {
					sheet = '';
					void tick().then(() => input?.focus());
				}}>{msg('close')}</button
			>
		</div>
	{/snippet}
	{#if !small}
		<aside class="pa-nav">
			<div class="pa-brand">{@render brand()}</div>
			<button class="pa-button" onclick={newChat}><Icon name="plus" />{msg('newChat')}</button>
			<nav>{@render navButtons()}</nav>
			<hr />
			<h2>{msg('chats')}</h2>
			{@render history()}
			<div class="pa-account">
				<span>{(session.data?.user?.name ?? '').slice(0, 1).toUpperCase()}</span>
				<div><b>{session.data?.user?.name}</b><small>{msg('workspace')}</small></div>
			</div>
			{#if !inWindow}<button class="pa-home" onclick={() => openInProgram('/')}
					><Icon name="home" />{msg('home')}</button
				>{/if}
		</aside>
	{:else}
		<header class="pa-mobile-header">
			<div class="pa-brand">{@render brand()}</div>
			<div class="pa-actions">
				<button class="pa-icon" aria-label={msg('openChats')} onclick={() => (sheet = 'history')}
					><Icon name="sidebar" /></button
				><button class="pa-tab-avatar" aria-label={msg('settings')} onclick={() => show('settings')}
					>{(session.data?.user?.name ?? '').slice(0, 1).toUpperCase()}</button
				>
			</div>
		</header>
	{/if}
	<main class="pa-main">
		{#if error}<div class="pa-error" role="alert">
				<Icon name="info" /><span>{error}</span><button
					class="pa-icon"
					aria-label={msg('retry')}
					onclick={() => {
						error = '';
						void polling?.now();
					}}><Icon name="refresh" /></button
				>
			</div>{/if}
		{#if !snapshot}<p class="pa-muted pa-pane">{msg('loading')}</p>
		{:else if tab === 'settings'}<SettingsView
				{snapshot}
				{settingsFocus}
				{model}
				{localAllowed}
				onmodelchange={() => modelRefresh++}
				{sources}
				{pending}
				onjob={fromSkill}
				ondraft={(s) => (draft = s)}
				onsave={save}
			/>
		{:else if tab === 'jobs'}<Jobs
				jobs={snapshot.jobs}
				caps={snapshot.capabilities}
				paused={snapshot.settings.paused}
				{select}
				{pending}
				toggle={() => void save({ paused: !snapshot?.settings.paused }).catch(() => {})}
			/>
		{:else}
			{#if chosen}<header class="pa-chat-head">
					<h1>{chosen.title}</h1>
					{#if rows.some((j) => j.status === 'waiting')}<span class="pa-status"
							><Icon name="lock" size={14} />{msg('needs')}</span
						>{/if}
				</header>{/if}
			<div class="pa-chat-scroll" bind:this={scroller}>
				<ChatView
					{phaseFor}
					chat={chosen}
					jobs={rows}
					caps={snapshot.capabilities}
					settings={snapshot.settings}
					{reduced}
					{select}
					onreview={reviewDraft}
					onedit={editDraft}
					onsource={sourceOpen}
					onexample={() => void submit(true)}
					onsettings={() => show('settings', 'permissions')}
					available={readyOptions}
					oncontext={() => (sheet = 'context')}
					onbackground={background}
					oncue={signal}
				/>
			</div>
			<form
				class="pa-compose"
				onsubmit={(e) => {
					e.preventDefault();
					void submit();
				}}
			>
				{#if tool === 'chat.ask' && selected && selected.effective !== 'never' && model.state !== 'ready'}
					<div class="pa-model-status" role="status">
						<span>{modelStatus(model)}</span>
						{#if model.state !== 'checking'}<button
								type="button"
								class="pa-link"
								onclick={() => show('settings', 'model')}
								>{msg('model')}<Icon name="chevronRight" size={14} /></button
							>{/if}
					</div>
				{/if}
				{#if sources.length || tool === 'chat.cloud' || office}<div class="pa-chips">
						{#if tool === 'chat.cloud' || office}<button
								type="button"
								onclick={() => (sheet = 'context')}
								>{selected?.title ?? tool}<Icon name="chevronDown" size={13} /></button
							>{/if}
						{#each sources as src, i}<button
								type="button"
								data-guide={'source-' + i}
								aria-label={msg('remove') + ' ' + sourceTitle(src)}
								onclick={() => removeSource(i)}
								>{sourceTitle(src)}<Icon name="close" size={13} /></button
							>{/each}
					</div>{/if}
				<div class="pa-input" data-guide="composer">
					<button
						class="pa-icon"
						type="button"
						aria-label={msg('sources') + ' / ' + msg('tool')}
						onclick={() => (sheet = 'context')}><Icon name="plus" /></button
					>
					<textarea
						rows="1"
						bind:this={input}
						bind:value={prompt}
						aria-label={msg('prompt')}
						placeholder={tool === 'chat.ask' && selected?.effective === 'never'
							? msg('modelUnavailable')
							: tool === 'chat.ask' && model.state === 'ready'
								? t('papim.model.ask', { model: model.model ?? '' })
								: msg('placeholder')}
						onkeydown={onkey}></textarea>
					<button
						type="button"
						class="pa-icon"
						aria-label={msg('mic')}
						onclick={() => {
							sheet = 'voice';
							signal(['composer', 'orb'], tool);
						}}><Icon name="mic" /></button
					>
					<button
						type="submit"
						class="pa-send"
						aria-label={msg('send')}
						disabled={pending ||
							!selected ||
							selected.effective === 'never' ||
							(office && !valid) ||
							!canSubmit ||
							((tool.startsWith('chat.') || tool === 'files.search') && !prompt.trim())}
						><Icon name="arrowUp" /></button
					>
				</div>
			</form>
		{/if}
	</main>
	{#if small}<nav class="pa-tabs" aria-label="Agent">{@render navButtons()}</nav>{/if}
	{#if detail && snapshot}{#if narrow}<Sheet title={msg('jobDetails')} onclose={closeDetail} {calm}
				><JobDetail
					job={detail}
					caps={snapshot.capabilities}
					{pending}
					onaction={act}
					onreview={reviewDraft}
					onsource={sourceOpen}
					onsettings={() => show('settings')}
					oncue={signal}
					first={firstId === detail.id}
				/></Sheet
			>
		{:else}<aside class="pa-inspector">
				<button class="pa-icon pa-close" aria-label={msg('close')} onclick={closeDetail}
					><Icon name="close" /></button
				><JobDetail
					job={detail}
					caps={snapshot.capabilities}
					{pending}
					onaction={act}
					onreview={reviewDraft}
					onsource={sourceOpen}
					onsettings={() => show('settings')}
					oncue={signal}
					first={firstId === detail.id}
				/>
			</aside>{/if}{/if}
	{#if sheet === 'history'}<Sheet title={msg('chats')} {calm} onclose={() => (sheet = '')}
			><button class="pa-button" onclick={newChat}><Icon name="plus" />{msg('newChat')}</button
			>{@render history()}<button class="pa-link" onclick={() => openInProgram('/')}
				><Icon name="home" />{msg('home')}</button
			></Sheet
		>
	{:else if sheet === 'context'}<Sheet
			title={msg('requestType')}
			{calm}
			onclose={() => (sheet = '')}>{@render context()}</Sheet
		>
	{:else if sheet === 'voice'}<VoiceInput
			{calm}
			caps={snapshot?.capabilities ?? []}
			admin={session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin'}
			onsettings={() => show('settings', 'permissions')}
			{model}
			onmodel={() => show('settings', 'model')}
			onturn={voiceTurn}
			onupdate={(job) => {
				if (snapshot && job.request.chatId === chatId)
					snapshot = { ...snapshot, jobs: snapshot.jobs.map((j) => (j.id === job.id ? job : j)) };
			}}
			onclose={() => {
				sheet = '';
				signal(['orb', 'composer'], tool);
			}}
			ontext={(s) => (prompt = (prompt + ' ' + s).trim())}
		/>{/if}
	{#if openedSource}<SourcePreview
			source={openedSource}
			{calm}
			onclose={() => (openedSource = null)}
		/>{/if}
	<MotionGuide {cue} mode={appearance?.motion ?? 'full'} {reduced} />
</div>
