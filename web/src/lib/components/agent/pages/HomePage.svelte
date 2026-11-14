<script lang="ts">
	// The start page of the KI-Agent (PLAN 90-4), the same on the desktop and
	// the phone: what waits for the person (approvals in chats, questions and
	// suggestions of the agent), what runs right now, how the NAS is doing
	// (numbers of titand, no model asked; owners and administrators only),
	// the next planned tasks, what planned tasks last did, and up to three
	// questions that fit the moment. Nothing is decided here without the
	// person: an approval leads to its chat, where the step is shown whole.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime, formatTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type {
		AgentView,
		ChatInfo,
		HomeState,
		InboxItem,
		NasHint,
		Schedule,
		Suggestion,
	} from '#lib/agent.ts';
	import PageShell from './PageShell.svelte';

	let {
		user,
		chats,
		suggestions,
		hints = [],
		show = true,
		phone = false,
		onunfold,
		onopen,
		onstop,
		onfill,
		onask,
		onshow,
		onshuffle,
		oncount,
	}: {
		user: string;
		chats: ChatInfo[];
		suggestions: Suggestion[];
		/** Warnings of the NAS of the last two days (updates among them). */
		hints?: NasHint[];
		/** Suggestions switched on (Einstellungen › Darstellung). */
		show?: boolean;
		phone?: boolean;
		onunfold?: () => void;
		onopen: (id: string) => void;
		onstop: (c: ChatInfo) => void;
		onfill: (text: string) => void;
		/** Opens a new chat with this question. */
		onask: (text: string) => void;
		onshow: (view: AgentView) => void;
		onshuffle?: () => void;
		/** Open suggestions of the agent (the number in the menu). */
		oncount?: (n: number) => void;
	} = $props();

	type Planned = Schedule & { next?: string };
	let items = $state<InboxItem[]>([]);
	let nas = $state<HomeState | null>(null);
	let planned = $state<Planned[]>([]);
	let error = $state('');
	let editing = $state<{ id: string; text: string } | null>(null);
	$effect(() => {
		api
			.get<{ items: InboxItem[] }>('/agent/inbox')
			.then((r) => (items = (r.items ?? []).reverse()))
			.catch((e) => (error = apiErrorText(e)));
		api
			.get<{ state: HomeState | null }>('/agent/home')
			.then((r) => (nas = r.state))
			.catch(() => (nas = null));
		api
			.get<{ schedules?: Planned[] }>('/agent/schedules')
			.then((r) => (planned = r.schedules ?? []))
			.catch(() => (planned = []));
	});
	$effect(() => oncount?.(items.length));

	const hour = new Date().getHours();
	const hello = $derived(
		hour < 11
			? t('agent.helloMorning', { name: user })
			: hour < 18
				? t('agent.helloDay', { name: user })
				: t('agent.helloEvening', { name: user }),
	);
	const today = new Intl.DateTimeFormat(undefined, {
		weekday: 'long',
		day: 'numeric',
		month: 'long',
	}).format(new Date());
	const waiting = $derived(chats.filter((c) => c.state === 'waiting' && !c.author));
	const running = $derived(chats.filter((c) => c.state === 'running' && !c.author));
	const open = $derived(waiting.length + items.length);
	const lead = $derived(
		open === 0
			? today
			: `${today} · ${open === 1 ? t('agent.home.openOne') : t('agent.home.open', { count: open })}`,
	);
	const next = $derived(
		planned
			.filter((s) => s.next && s.enabled)
			.sort((a, b) => (a.next ?? '').localeCompare(b.next ?? ''))
			.slice(0, 3),
	);
	// What planned tasks, reminders and events last did: their chats.
	const done = $derived(
		chats
			.filter(
				(c) =>
					!c.author &&
					c.state !== 'running' &&
					c.state !== 'waiting' &&
					['schedule', 'reminder', 'event'].includes(c.origin ?? ''),
			)
			.sort((a, b) => b.updated.localeCompare(a.updated))
			.slice(0, 3),
	);

	// ----- suggestions of the agent (formerly the panel "Vorschläge") -----
	const inboxIcon: Record<string, IconName> = {
		memory: 'bulb',
		conflict: 'activity',
		fade: 'clock',
		skill: 'zap',
		forecast: 'disk',
		alert: 'bell',
		deadline: 'clock',
	};
	const kindText = (k: string) =>
		k === 'deadline' ? t('agent.f.docs.inboxKind') : t(`agent.inbox.kind.${k}` as MessageKey);
	const asks = (k: InboxItem['kind']) => k === 'alert' || k === 'forecast';
	const yesLabel = (k: InboxItem['kind']) =>
		k === 'fade' || k === 'conflict'
			? t('agent.inbox.remove')
			: asks(k)
				? t('agent.inbox.ask')
				: t('agent.inbox.take');
	const noLabel = (k: InboxItem['kind']) =>
		k === 'fade' || k === 'conflict' ? t('agent.inbox.keep') : t('agent.inbox.no');
	async function decide(item: InboxItem, accept: boolean, text = '') {
		error = '';
		try {
			await api.post(`/agent/inbox/${item.id}`, { accept, text });
			items = items.filter((x) => x.id !== item.id);
			editing = null;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	function yes(item: InboxItem) {
		if (asks(item.kind)) {
			void decide(item, false);
			onask(t('agent.inbox.askText', { title: item.title }));
		} else void decide(item, true, editing?.id === item.id ? editing.text : '');
	}

	// ----- the state of the NAS, as rows -----
	type Tone = 'ok' | 'warn' | 'error' | 'info';
	type Row = {
		id: string;
		icon: IconName;
		title: string;
		desc: string;
		tone: Tone;
		chip: string;
		href: string;
	};
	const chipOf = (tone: Tone) =>
		tone === 'ok'
			? t('agent.home.good')
			: tone === 'error'
				? t('agent.home.bad')
				: t('agent.home.watch');
	const rows = $derived.by<Row[]>(() => {
		const out: Row[] = [];
		if (!nas) return out;
		const vols = nas.volumes;
		if (vols.length) {
			// Space: the fullest volume decides; a trend names the days left.
			const mounted = vols.filter((v) => v.mounted);
			const full = [...mounted].sort((a, b) => b.percent - a.percent)[0];
			const soon = mounted.filter((v) => v.daysLeft).sort((a, b) => a.daysLeft! - b.daysLeft!)[0];
			if (full) {
				const tone: Tone =
					full.percent >= 95 ? 'error' : full.percent >= 85 || soon ? 'warn' : 'ok';
				out.push({
					id: 'space',
					icon: 'disk',
					title: t('agent.home.space'),
					desc:
						t('agent.home.spaceUsed', { name: full.name, percent: full.percent }) +
						(soon
							? ' · ' +
								(soon.name === full.name
									? t('agent.home.spaceDaysSame', { days: soon.daysLeft! })
									: t('agent.home.spaceDays', { name: soon.name, days: soon.daysLeft! }))
							: ''),
					tone,
					chip: chipOf(tone),
					href: '/storage',
				});
			}
			// Disks and RAID: every volume that is not simply there and whole.
			const failed = vols.filter((v) => ['failed', 'missing'].includes(v.state) || !v.mounted);
			const weak = vols.filter((v) => v.state === 'degraded');
			const busy = vols.filter((v) =>
				['rebuilding', 'syncing', 'checking', 'reshaping'].includes(v.state),
			);
			const tone: Tone = failed.length
				? 'error'
				: weak.length
					? 'warn'
					: busy.length
						? 'info'
						: 'ok';
			const names = (list: typeof vols) => list.map((v) => v.name).join(', ');
			out.push({
				id: 'raid',
				icon: 'shield',
				title: t('agent.home.raid'),
				desc: failed.length
					? t('agent.home.raidFailed', { names: names(failed) })
					: weak.length
						? t('agent.home.raidDegraded', { names: names(weak) })
						: busy.length
							? t('agent.home.raidBusy', { names: names(busy) })
							: vols.length === 1
								? t('agent.home.raidOkOne')
								: t('agent.home.raidOk', { count: vols.length }),
				tone,
				chip: tone === 'info' ? t('agent.home.busy') : chipOf(tone),
				href: '/storage',
			});
		}
		if (nas.backups) {
			const b = nas.backups;
			const tone: Tone = b.failing ? 'warn' : b.lastOK ? 'ok' : 'info';
			out.push({
				id: 'backups',
				icon: 'archive',
				title: t('agent.home.backups'),
				desc:
					(b.lastOK
						? t('agent.home.backupLast', { time: formatDateTime(new Date(b.lastOK)) })
						: t('agent.home.backupNever')) +
					' · ' +
					(b.failing
						? t('agent.home.backupFailing', { count: b.failing, total: b.jobs })
						: b.running
							? t('agent.home.backupRunning', { count: b.running })
							: b.jobs === 1
								? t('agent.home.backupJobsOne')
								: t('agent.home.backupJobs', { count: b.jobs })),
				tone,
				chip: tone === 'info' ? t('agent.home.none') : chipOf(tone),
				href: '/settings/backups',
			});
		}
		if (nas.apps) {
			const a = nas.apps;
			out.push({
				id: 'apps',
				icon: 'box',
				title: t('agent.home.apps'),
				desc:
					t('agent.home.appsRunning', { count: a.running }) +
					(a.stopped ? ' · ' + t('agent.home.appsStopped', { count: a.stopped }) : ''),
				tone: 'ok',
				chip: chipOf('ok'),
				href: '/docker',
			});
		}
		const updates = hints.filter((h) => h.kind === 'update_available');
		if (updates.length)
			out.push({
				id: 'updates',
				icon: 'download',
				title: t('agent.home.updates'),
				desc: updates.map((u) => u.title).join(' · '),
				tone: 'info',
				chip: t('agent.home.updatesOpen', { count: updates.length }),
				href: '/settings/updates',
			});
		return out;
	});
</script>

<PageShell title={hello} plain {lead} {phone} {onunfold}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

	{#if open}
		<SettingGroup title={t('agent.home.waiting')}>
			{#each waiting as chat (chat.id)}
				<div class="srow">
					<div class="srow-head">
						<span class="srow-ico warn"><Icon name="shield" size={19} /></span>
						<div class="srow-text">
							<span class="srow-title">{t('agent.home.approval', { title: chat.title })}</span>
							<p class="srow-desc">
								{t('agent.home.approvalDesc', { time: formatDateTime(new Date(chat.updated)) })}
							</p>
						</div>
						<div class="srow-ctl">
							<button type="button" class="btn btn-primary" onclick={() => onopen(chat.id)}
								>{t('agent.home.look')}</button
							>
						</div>
					</div>
				</div>
			{/each}
			{#each items as item (item.id)}
				<div class="srow">
					<div class="srow-head">
						<span class="srow-ico"><Icon name={inboxIcon[item.kind] ?? 'bell'} size={19} /></span>
						<div class="srow-text">
							{#if editing?.id === item.id}
								<textarea
									class="field"
									rows="2"
									maxlength="500"
									aria-label={t('agent.edit')}
									bind:value={editing.text}></textarea>
							{:else}
								<span class="srow-title">{item.title}</span>
							{/if}
							<p class="srow-desc">
								{kindText(item.kind)} · {formatDateTime(new Date(item.created))}{item.body
									? ' · ' + item.body
									: ''}
							</p>
						</div>
						<div class="srow-ctl">
							{#if item.kind === 'memory' && editing?.id !== item.id}
								<button
									type="button"
									class="btn"
									onclick={() => (editing = { id: item.id, text: item.title })}
									>{t('agent.edit')}</button
								>
							{/if}
							<button type="button" class="btn" onclick={() => decide(item, false)}
								>{noLabel(item.kind)}</button
							>
							<button type="button" class="btn" onclick={() => yes(item)}
								>{yesLabel(item.kind)}</button
							>
						</div>
					</div>
				</div>
			{/each}
		</SettingGroup>
	{/if}

	{#if running.length}
		<SettingGroup title={t('agent.home.running')}>
			{#each running as chat (chat.id)}
				<div class="srow">
					<div class="srow-head">
						<span class="srow-ico live"><Icon name="activity" size={19} /></span>
						<div class="srow-text">
							<span class="srow-title">{chat.title}</span>
							<p class="srow-desc">{t('agent.working')}</p>
						</div>
						<div class="srow-ctl">
							<button type="button" class="btn" onclick={() => onopen(chat.id)}
								>{t('agent.home.look')}</button
							>
							<button
								type="button"
								class="btn"
								aria-label={t('agent.stopChat', { title: chat.title })}
								onclick={() => onstop(chat)}>{t('agent.stop')}</button
							>
						</div>
					</div>
				</div>
			{/each}
		</SettingGroup>
	{/if}

	<div class="two" class:single={!rows.length || (!next.length && !done.length)}>
		{#if rows.length && nas}
			<SettingGroup title={t('agent.home.state')}>
				{#snippet tools()}<span class="when"
						>{t('agent.home.asOf', { time: formatTime(new Date(nas!.at)) })}</span
					>{/snippet}
				{#each rows as r (r.id)}
					<a class="srow" href={r.href}>
						<div class="srow-head">
							<span class="srow-ico {r.tone === 'warn' || r.tone === 'error' ? r.tone : ''}"
								><Icon name={r.icon} size={19} /></span
							>
							<div class="srow-text">
								<span class="srow-title">{r.title}</span>
								<p class="srow-desc">{r.desc}</p>
							</div>
							<span class="srow-ctl"><span class="chip {r.tone}">{r.chip}</span></span>
							<span class="srow-chev"><Icon name="chevronRight" size={16} /></span>
						</div>
					</a>
				{/each}
			</SettingGroup>
		{/if}
		{#if next.length || done.length}
			<div class="stack">
				{#if next.length}
					<SettingGroup title={t('agent.home.next')}>
						{#snippet tools()}<button type="button" class="more" onclick={() => onshow('planned')}
								>{t('agent.home.all', { count: planned.length })}</button
							>{/snippet}
						{#each next as s (s.id)}
							<button type="button" class="srow" onclick={() => onshow('planned')}>
								<div class="srow-head">
									<span class="srow-ico"
										><Icon name={s.kind === 'reminder' ? 'bell' : 'clock'} size={19} /></span
									>
									<div class="srow-text">
										<span class="srow-title"
											>{s.kind === 'morning' ? t('agent.morning') : s.name}</span
										>
										<p class="srow-desc">
											{s.kind === 'reminder'
												? t('agent.home.reminder')
												: s.kind === 'morning'
													? t('agent.home.report')
													: t('agent.home.task')} · {formatDateTime(new Date(s.next!))}
										</p>
									</div>
									<span class="srow-chev"><Icon name="chevronRight" size={16} /></span>
								</div>
							</button>
						{/each}
					</SettingGroup>
				{/if}
				{#if done.length}
					<SettingGroup title={t('agent.home.done')}>
						{#each done as chat (chat.id)}
							<button type="button" class="srow" onclick={() => onopen(chat.id)}>
								<div class="srow-head">
									<span class="srow-ico ok"><Icon name="check" size={19} /></span>
									<div class="srow-text">
										<span class="srow-title">{chat.title}</span>
										<p class="srow-desc">
											{formatDateTime(new Date(chat.updated))}{chat.preview
												? ' · ' + chat.preview
												: ''}
										</p>
									</div>
									<span class="srow-chev"><Icon name="chevronRight" size={16} /></span>
								</div>
							</button>
						{/each}
					</SettingGroup>
				{/if}
			</div>
		{/if}
	</div>

	{#if show && suggestions.length}
		<SettingGroup title={t('agent.fitsNow')}>
			{#snippet tools()}{#if onshuffle}<button type="button" class="more" onclick={onshuffle}
						><Icon name="refresh" size={13} />{t('agent.otherSuggestions')}</button
					>{/if}{/snippet}
			<div class="ideas">
				{#each suggestions as s (s.text)}
					<button type="button" class="srow" onclick={() => onfill(s.text)}>
						<div class="srow-head">
							<span class="srow-ico {s.tone === 'warn' ? 'warn' : ''}"
								><Icon name={s.icon ?? 'sparkle'} size={19} /></span
							>
							<div class="srow-text">
								<span class="srow-title"
									>{s.title}{#if s.tag}
										<span class="chip {s.tone ?? 'info'}">{s.tag}</span>{/if}</span
								>
								<p class="srow-desc">{s.sub}</p>
							</div>
						</div>
					</button>
				{/each}
			</div>
		</SettingGroup>
	{/if}
</PageShell>

<style>
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-4);
		align-items: start;
	}
	.two.single {
		grid-template-columns: 1fr;
	}
	.two:empty {
		display: none;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		min-width: 0;
	}
	.ideas {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 4px;
	}
	button.srow {
		display: block;
		width: 100%;
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
	}
	button.srow:hover {
		background: var(--srow-hover, color-mix(in srgb, var(--text) 5%, var(--srow-bg)));
	}
	button.srow:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.when,
	.more {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.more {
		min-height: 32px;
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.srow-ico.warn {
		color: var(--status-warn);
	}
	.srow-ico.ok {
		color: var(--status-ok);
	}
	.srow-ico.error {
		color: var(--status-error);
	}
	.srow-ico.live {
		color: var(--accent);
	}
	.srow-text .field {
		width: 100%;
	}
	@media (max-width: 899px) {
		.two,
		.ideas {
			grid-template-columns: 1fr;
		}
		.two {
			gap: var(--space-3);
		}
		.stack {
			gap: var(--space-3);
		}
	}
</style>
