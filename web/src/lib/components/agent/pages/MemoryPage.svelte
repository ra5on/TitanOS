<script lang="ts">
	// "Gedächtnis" of the KI-Agent as a page (PLAN 90-4): what it remembered,
	// to search, ordered by topic, last use or age, each with where it came
	// from and when a message last touched it. A pinned memory never fades
	// and stays when the memory is emptied. On the phone a row is swiped:
	// to the right pins, to the left deletes (asked first). Below the list
	// the settings of the memory, which stood under "Eigeninitiative" before.
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { api } from '#lib/api.ts';
	import { swipe } from '#lib/agent-swipe.ts';
	import { formatDate } from '#lib/format.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import {
		defaultInitiative,
		sanitizeInitiative,
		type ChatInfo,
		type Initiative,
		type Memory,
		type Project,
	} from '#lib/agent.ts';
	import PageShell from './PageShell.svelte';

	let {
		projects = [],
		chats = [],
		query: startQuery = '',
		draft: startDraft = '',
		phone = false,
		gestures = true,
		onunfold,
		onopenchat,
	}: {
		projects?: Project[];
		chats?: ChatInfo[];
		/** What to look for at first (from the search of the side bar). */
		query?: string;
		/** A new memory to write, with this text ("Das merken" of a message). */
		draft?: string;
		phone?: boolean;
		gestures?: boolean;
		onunfold?: () => void;
		onopenchat: (id: string) => void;
	} = $props();

	let memory = $state<Memory[]>([]);
	let loaded = $state(false);
	let error = $state('');
	let query = $state((() => startQuery)());
	let order = $state<'topic' | 'used' | 'newest'>('topic');
	let editing = $state<{ id: string; text: string; topic: string } | null>(
		(() => (startDraft ? { id: 'new', text: startDraft.slice(0, 500), topic: '' } : null))(),
	);
	let confirm = $state<{ kind: 'delete'; memory: Memory } | { kind: 'clear' } | null>(null);

	async function load() {
		try {
			memory = (await api.get<{ memory: Memory[] }>('/agent/memory')).memory;
			loaded = true;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
	});
	async function run(fn: () => Promise<unknown>) {
		error = '';
		try {
			await fn();
			editing = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const save = () =>
		editing &&
		run(() =>
			api.put(`/agent/memory/${editing!.id}`, { text: editing!.text, topic: editing!.topic }),
		);
	const pin = (m: Memory) => run(() => api.put(`/agent/memory/${m.id}`, { pinned: !m.pinned }));

	// ----- the settings of the memory (GET/PUT /agent/initiative) -----
	let ini = $state<Initiative>(defaultInitiative());
	let iniLoaded = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		api
			.get<unknown>('/agent/initiative')
			// Saving starts only after the settings were read.
			.then((v) => {
				ini = sanitizeInitiative(v);
				iniLoaded = true;
			})
			.catch((e) => (error = apiErrorText(e)));
	});
	let iniSaved = '';
	$effect(() => {
		const body = $state.snapshot(ini);
		if (!iniLoaded) return;
		// Only what the person changed is written, not what was just read.
		const now = JSON.stringify(body);
		if (!iniSaved) iniSaved = now;
		if (now === iniSaved) return;
		clearTimeout(timer);
		timer = setTimeout(() => {
			api
				.put('/agent/initiative', body)
				.then(() => (iniSaved = now))
				.catch((e) => (error = apiErrorText(e)));
		}, 500);
	});
	// The slider's right end: one step past 24 months means never.
	const FADE_NEVER = 25;
	const fadeText = (months: number) =>
		!months
			? t('agent.init.fadeOff')
			: months === 1
				? t('agent.init.fadeHintOne')
				: t('agent.init.fadeHint', { months });
	const fadeValue = (months: number) =>
		!months
			? t('agent.mem.fadeNever')
			: months === 1
				? t('agent.mem.fadeMonth')
				: t('agent.mem.fadeMonths', { months });

	// ----- what a row says -----
	const DAY = 86_400_000;
	const fresh = (m: Memory) => Math.max(Date.parse(m.at), m.used ? Date.parse(m.used) : 0);
	function ago(iso: string): string {
		const days = Math.floor((Date.now() - Date.parse(iso)) / DAY);
		if (days <= 0) return t('agent.mem.today');
		const rel = new Intl.RelativeTimeFormat(i18n.locale, { numeric: 'auto' });
		if (days < 14) return rel.format(-days, 'day');
		if (days < 60) return rel.format(-Math.round(days / 7), 'week');
		if (days < 730) return rel.format(-Math.round(days / 30), 'month');
		return rel.format(-Math.round(days / 365), 'year');
	}
	const project = (id?: string) => projects.find((p) => p.id === id);
	const chatTitle = (id?: string) => chats.find((c) => c.id === id && !c.author)?.title ?? '';
	/** The question "noch aktuell?" comes with the next night. */
	const fading = (m: Memory) =>
		!m.pinned && ini.fadeMonths > 0 && Date.now() - fresh(m) > (ini.fadeMonths - 1) * 30 * DAY;
	function topicOf(m: Memory): string {
		if (m.topic) return m.topic;
		const p = project(m.project);
		return p ? `${p.icon ? p.icon + ' ' : ''}${p.name}` : t('agent.mem.noTopic');
	}

	const shown = $derived.by(() => {
		const q = query.trim().toLowerCase();
		return q ? memory.filter((m) => (m.text + ' ' + topicOf(m)).toLowerCase().includes(q)) : memory;
	});
	const groups = $derived.by<{ title: string; rows: Memory[] }[]>(() => {
		if (order === 'used')
			return [{ title: '', rows: [...shown].sort((a, b) => fresh(b) - fresh(a)) }];
		if (order === 'newest')
			return [{ title: '', rows: [...shown].sort((a, b) => Date.parse(b.at) - Date.parse(a.at)) }];
		const by = new Map<string, Memory[]>();
		for (const m of shown) by.set(topicOf(m), [...(by.get(topicOf(m)) ?? []), m]);
		const none = t('agent.mem.noTopic');
		return [...by.entries()]
			.sort(([a], [b]) => (a === none ? 1 : b === none ? -1 : a.localeCompare(b, i18n.locale)))
			.map(([title, rows]) => ({
				title,
				rows: rows.sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || fresh(b) - fresh(a)),
			}));
	});
	const topics = $derived([...new Set(memory.map((m) => m.topic).filter(Boolean))] as string[]);
</script>

{#snippet editor()}
	<form
		class="edit"
		onsubmit={(e) => {
			e.preventDefault();
			void save();
		}}
	>
		<textarea
			class="field"
			rows="2"
			maxlength="500"
			bind:value={editing!.text}
			aria-label={t('agent.mem.text')}
			placeholder={t('agent.memoryNewHint')}></textarea>
		<input
			class="field"
			maxlength="40"
			list="mem-topics"
			bind:value={editing!.topic}
			aria-label={t('agent.mem.topic')}
			placeholder={t('agent.mem.topicHint')}
		/>
		<div class="edit-acts">
			<button type="button" class="btn" onclick={() => (editing = null)}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={!editing!.text.trim()}
				>{t('common.save')}</button
			>
		</div>
	</form>
{/snippet}

{#snippet row(m: Memory)}
	<div
		class="swipe"
		use:swipe={{
			enabled: phone && gestures && editing?.id !== m.id,
			right: m.pinned ? t('agent.unpin') : t('agent.pin'),
			left: t('common.delete'),
			onswipe: (side) =>
				side === 'right' ? void pin(m) : (confirm = { kind: 'delete', memory: m }),
			onmenu: () => (editing = { id: m.id, text: m.text, topic: m.topic ?? '' }),
		}}
	>
		<div class="srow">
			<div class="srow-head">
				<span class="srow-ico" class:pinned={m.pinned}
					><Icon name={m.pinned ? 'pin' : 'bulb'} size={19} /></span
				>
				<div class="srow-text">
					{#if editing?.id === m.id}
						{@render editor()}
					{:else}
						<span class="srow-title normal">{m.text}</span>
						<p class="srow-desc">
							{#if m.chat && chatTitle(m.chat)}<button
									type="button"
									class="src"
									onclick={() => onopenchat(m.chat!)}
									>{t('agent.mem.fromChat', { title: chatTitle(m.chat) })}</button
								>{' · '}{/if}{formatDate(new Date(m.at))}{order !== 'topic' &&
							topicOf(m) !== t('agent.mem.noTopic')
								? ' · ' + topicOf(m)
								: ''} ·
							{m.used ? t('agent.mem.used', { when: ago(m.used) }) : t('agent.mem.unused')}
							{#if m.pinned}<span class="chip info">{t('agent.mem.pinned')}</span>{/if}
							{#if fading(m)}<span class="chip warn">{t('agent.mem.fading')}</span>{/if}
							{#if m.untrusted}<span class="chip warn" title={t('agent.untrustedHint')}
									>{t('agent.untrusted')}</span
								>{/if}
						</p>
					{/if}
				</div>
				{#if editing?.id !== m.id}
					<div class="srow-ctl acts">
						<button
							type="button"
							class="ib"
							class:on={m.pinned}
							aria-pressed={!!m.pinned}
							aria-label={m.pinned ? t('agent.unpin') : t('agent.pin')}
							title={m.pinned ? t('agent.unpin') : t('agent.pin')}
							onclick={() => pin(m)}><Icon name="pin" size={16} /></button
						>
						<button
							type="button"
							class="ib"
							aria-label={t('agent.edit')}
							title={t('agent.edit')}
							onclick={() => (editing = { id: m.id, text: m.text, topic: m.topic ?? '' })}
							><Icon name="edit" size={16} /></button
						>
						<button
							type="button"
							class="ib"
							aria-label={t('common.delete')}
							title={t('common.delete')}
							onclick={() => (confirm = { kind: 'delete', memory: m })}
							><Icon name="trash" size={16} /></button
						>
					</div>
				{/if}
			</div>
		</div>
	</div>
{/snippet}

<PageShell title={t('agent.memory')} lead={t('agent.mem.lead')} {phone} {onunfold}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<datalist id="mem-topics"
		>{#each topics as tp (tp)}<option value={tp}></option>{/each}</datalist
	>

	<div class="mem-bar">
		<label class="search">
			<Icon name="search" size={16} />
			<input
				type="search"
				bind:value={query}
				placeholder={t('agent.mem.search')}
				aria-label={t('agent.mem.search')}
			/>
		</label>
		<span class="seg" role="radiogroup" aria-label={t('agent.mem.order')}>
			{#each ['topic', 'used', 'newest'] as const as o (o)}
				<label class:on={order === o}
					><input type="radio" bind:group={order} value={o} />{t(`agent.mem.order.${o}`)}</label
				>
			{/each}
		</span>
		<button type="button" class="btn" onclick={() => (editing = { id: 'new', text: '', topic: '' })}
			><Icon name="plus" size={15} />{t('agent.mem.add')}</button
		>
	</div>

	{#if editing?.id === 'new'}
		<SettingGroup title={t('agent.mem.new')}>
			<div class="srow">
				<div class="srow-head">
					<span class="srow-ico"><Icon name="bulb" size={19} /></span>
					<div class="srow-text">{@render editor()}</div>
				</div>
			</div>
		</SettingGroup>
	{/if}

	{#each groups as g (g.title)}
		<SettingGroup title={g.title || undefined}>
			{#snippet tools()}{#if g.title}<span class="when"
						>{t('agent.mem.count', { count: g.rows.length })}</span
					>{/if}{/snippet}
			{#each g.rows as m (m.id)}{@render row(m)}{/each}
		</SettingGroup>
	{/each}
	{#if loaded && !shown.length}
		<p class="muted none">{query.trim() ? t('agent.mem.noneFound') : t('agent.memoryEmpty')}</p>
	{/if}

	<SettingGroup title={t('agent.mem.how')}>
		<SettingRow
			icon="bulb"
			for="mem-auto"
			title={t('agent.init.memoryAuto')}
			desc={t('agent.init.memoryAutoHint')}
		>
			{#snippet control()}<input
					id="mem-auto"
					type="checkbox"
					class="switch"
					role="switch"
					bind:checked={ini.memoryAuto}
				/>{/snippet}
		</SettingRow>
		<SettingRow
			icon="info"
			for="mem-conflicts"
			title={t('agent.init.conflicts')}
			desc={t('agent.init.conflictsHint')}
		>
			{#snippet control()}<input
					id="mem-conflicts"
					type="checkbox"
					class="switch"
					role="switch"
					bind:checked={ini.conflicts}
				/>{/snippet}
		</SettingRow>
		<SettingRow
			icon="clock"
			for="mem-fade"
			title={t('agent.init.fade')}
			desc={`${fadeText(ini.fadeMonths)} · ${t('agent.mem.fadePinned')}`}
		>
			<!-- 1–24 months, the right end is "unbegrenzt" (never ask). Stored as
			     0, which older versions read as "off" (PLAN 90-2). -->
			{#snippet control()}<input
					id="mem-fade"
					type="range"
					min="1"
					max={FADE_NEVER}
					step="1"
					value={ini.fadeMonths || FADE_NEVER}
					aria-valuetext={fadeValue(ini.fadeMonths)}
					oninput={(e) => {
						const n = Number(e.currentTarget.value);
						ini.fadeMonths = n >= FADE_NEVER ? 0 : n;
					}}
				/><span class="val">{fadeValue(ini.fadeMonths)}</span>{/snippet}
		</SettingRow>
		<SettingRow icon="trash" title={t('agent.mem.clear')} desc={t('agent.mem.clearDesc')}>
			{#snippet control()}<button
					type="button"
					class="btn btn-danger-outline"
					disabled={!memory.some((m) => !m.pinned)}
					onclick={() => (confirm = { kind: 'clear' })}>{t('agent.mem.clearAsk')}</button
				>{/snippet}
		</SettingRow>
	</SettingGroup>
</PageShell>

{#if confirm}
	{@const c = confirm}
	<Dialog
		title={c.kind === 'clear' ? t('agent.mem.clear') : t('agent.mem.deleteTitle')}
		onclose={() => (confirm = null)}
	>
		<p>{c.kind === 'clear' ? t('agent.mem.clearSure') : `„${c.memory.text}“`}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					// The path first: `c` follows `confirm`, which closes the dialog.
					const path = c.kind === 'clear' ? '/agent/memory' : `/agent/memory/${c.memory.id}`;
					confirm = null;
					void run(() => api.delete(path));
				}}>{c.kind === 'clear' ? t('agent.mem.clearDo') : t('common.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.mem-bar {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex-wrap: wrap;
	}
	.search {
		flex: 1;
		min-width: 180px;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 36px;
		padding: 0 var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
		color: var(--muted);
	}
	.search:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	.search input {
		flex: 1;
		min-width: 0;
		min-height: 34px;
		border: 0;
		outline: 0;
		padding: 0;
		background: none;
		color: var(--text);
		font: inherit;
	}
	.seg {
		display: inline-flex;
		padding: 3px;
		border-radius: var(--radius-ctl);
		background: var(--track);
		font-size: var(--text-base);
	}
	.seg label {
		padding: var(--space-1) var(--space-3);
		border-radius: calc(var(--radius-ctl) - 2px);
		color: var(--muted);
		cursor: pointer;
	}
	.seg label.on {
		background: var(--surface);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
	}
	.seg input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.seg label:has(:focus-visible) {
		outline: 2px solid var(--accent);
	}
	.when {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.none {
		margin: 0;
	}
	.srow-ico.pinned {
		color: var(--accent);
	}
	.srow-title.normal {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.srow-desc .chip {
		margin-left: var(--space-1);
	}
	.src {
		border: 0;
		padding: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.acts {
		gap: var(--space-1-5);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.ib:hover {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.ib.on {
		color: var(--accent);
		border-color: color-mix(in srgb, var(--accent) 50%, transparent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.edit {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.edit .field {
		width: 100%;
	}
	.edit-acts {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.val {
		min-width: 86px;
		text-align: right;
	}
	/* Swiping a memory aside on the phone, like a chat in the menu. */
	.swipe {
		position: relative;
		border-radius: var(--srow-radius);
		touch-action: pan-y;
	}
	.swipe::before {
		content: attr(data-label);
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		padding: 0 var(--space-4);
		border-radius: var(--srow-radius);
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--on-accent);
		background: var(--accent);
		opacity: 0;
	}
	.swipe:global([data-side='left'])::before {
		justify-content: flex-end;
		color: var(--error-text);
		background: var(--error-bg);
	}
	.swipe:global([data-side='left'])::before,
	.swipe:global([data-side='right'])::before {
		opacity: 0.75;
	}
	.swipe:global([data-armed='true'])::before {
		opacity: 1;
	}
	.swipe > .srow {
		position: relative;
		transform: translateX(var(--dx, 0px));
		transition: transform 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	.swipe:global([data-side='left']) > .srow,
	.swipe:global([data-side='right']) > .srow {
		background: var(--surface);
	}
	.swipe:global(.dragging) > .srow {
		transition: none;
	}
	@media (max-width: 760px) {
		.seg {
			display: flex;
			flex-basis: 100%;
			order: 3;
		}
		.seg label {
			flex: 1;
			text-align: center;
			min-height: 36px;
			display: grid;
			place-items: center;
		}
		.search,
		.mem-bar .btn {
			min-height: 44px;
		}
		.search input {
			font-size: max(16px, 1rem);
		}
		.ib {
			width: 44px;
			height: 44px;
		}
		.srow-desc .chip {
			margin: var(--space-1) var(--space-1) 0 0;
		}
	}
</style>
