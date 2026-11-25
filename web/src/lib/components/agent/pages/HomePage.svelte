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
	import {
		activeSchedules,
		type AgentView,
		type ChatInfo,
		type HomeState,
		type InboxItem,
		type NasHint,
		type Schedule,
		type Suggestion,
	} from '#lib/agent.ts';
	import DayJournal from './DayJournal.svelte';
	import PageShell from './PageShell.svelte';

	let {
		user,
		chats,
		suggestions,
		hints = [],
		show = true,
		fresh = false,
		preferDetails = false,
		phone = false,
		onunfold,
		onapproval,
		calm = false,
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
		/** A new chat: only the question and up to three things to ask. */
		fresh?: boolean;
		preferDetails?: boolean;
		phone?: boolean;
		onunfold?: () => void;
		onapproval: (id: string) => void;
		calm?: boolean;
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

	// The greeting changes with the time of day and from visit to visit
	// (owner 15 Nov 2026): four sentences per part of the day, two more at
	// the weekend; late at night it is the one for night owls.
	const now = new Date();
	const hour = now.getHours();
	const slot =
		hour >= 23 || hour < 5 ? 'night' : hour < 11 ? 'morning' : hour < 18 ? 'day' : 'evening';
	const weekend = [0, 6].includes(now.getDay()) && slot === 'day';
	const pick = Math.floor(Math.random() * (weekend ? 6 : 4));
	const hello = $derived(
		t(
			(pick >= 4
				? `agent.greet.weekend.${pick - 3}`
				: `agent.greet.${slot}.${pick + 1}`) as MessageKey,
			{ name: user },
		),
	);
	let overviewOpen = $derived(preferDetails);
	const running = $derived(chats.filter((c) => c.state === 'running' && !c.author));
	const overviewId = $props.id();
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
	// Put warnings first. For a quiet NAS, show the latest backup and space.
	const highlights = $derived.by(() => {
		const priority: Record<Tone, number> = { error: 0, warn: 1, info: 2, ok: 3 };
		return [...rows]
			.sort(
				(a, b) =>
					priority[a.tone] - priority[b.tone] ||
					Number(b.id === 'backups') - Number(a.id === 'backups'),
			)
			.slice(0, 2);
	});
	const hasDetails = $derived(
		!!(items.length || running.length || next.length || done.length || rows.length || error),
	);
</script>

{#snippet line(
	icon: IconName,
	title: string,
	sub: string,
	tone: Tone | 'plain' = 'plain',
	actionIcon: IconName = 'chevronRight',
)}
	<span class="symbol" class:attention={tone === 'warn' || tone === 'error'}
		><Icon name={icon} size={17} /></span
	>
	<span class="line-text"><strong>{title}</strong><small>{sub}</small></span>
	<span class="line-end"><Icon name={actionIcon} size={14} /></span>
{/snippet}

<PageShell
	title={fresh ? t('agent.freshTitle') : hello}
	plain
	hideTitle={chats.length > 0 || phone}
	{phone}
	{onunfold}
>
	<DayJournal {chats} {onopen} {onapproval} {calm} />
	<div class="home-grid">
		{#if show && suggestions.length}
			<section class="preview" aria-label={t('agent.night.ideas')}>
				<header>
					<h2>{t('agent.night.ideas')}</h2>
					{#if onshuffle}<button
							type="button"
							class="header-action"
							aria-label={t('agent.otherSuggestions')}
							title={t('agent.otherSuggestions')}
							onclick={onshuffle}><Icon name="refresh" size={15} /></button
						>{/if}
				</header>
				<div class="preview-list ideas" aria-live="polite">
					{#each suggestions as suggestion}
						<button class="preview-row" type="button" onclick={() => onfill(suggestion.text)}>
							{@render line(
								suggestion.icon ?? 'sparkle',
								suggestion.title,
								suggestion.tag ? suggestion.sub + ' · ' + suggestion.tag : suggestion.sub,
								suggestion.tone ?? 'plain',
								'plus',
							)}
						</button>
					{/each}
				</div>
			</section>
		{/if}
		<section class="preview" aria-label={t('agent.night.overview')}>
			<header>
				<h2>{t('agent.night.overview')}</h2>
				{#if hasDetails}<button
						type="button"
						class="header-action"
						aria-expanded={overviewOpen}
						aria-controls={overviewId}
						onclick={() => (overviewOpen = !overviewOpen)}
						>{overviewOpen ? t('agent.home.less') : t('agent.home.details130')}
						<Icon name={overviewOpen ? 'chevronUp' : 'chevronDown'} size={12} /></button
					>{/if}
			</header>
			<div id={overviewId}>
				{#if !overviewOpen || !hasDetails}
					<div class="preview-list summary">
						{#if items.length}<button
								type="button"
								class="preview-row"
								onclick={() => (overviewOpen = true)}
							>
								{@render line(
									'bell',
									t('agent.home.waiting') + ' · ' + items.length,
									items[0].title,
									'warn',
								)}
							</button>{/if}
						{#if running.length}<button
								type="button"
								class="preview-row"
								onclick={() => onopen(running[0].id)}
							>
								{@render line(
									'activity',
									running[0].title,
									t('agent.home.runningCount', { count: running.length }),
								)}
							</button>{/if}
						{#if next[0]}
							<button type="button" class="preview-row" onclick={() => onshow('planned')}>
								{@render line(
									'clock',
									next[0].kind === 'morning' ? t('agent.morning') : next[0].name,
									t('agent.home.next') + ' · ' + formatDateTime(new Date(next[0].next!)),
								)}
							</button>
						{/if}
						{#each highlights as r (r.id)}
							<a class="preview-row" href={r.href}
								>{@render line(r.icon, r.title, r.desc, r.tone)}</a
							>
						{/each}
						{#if done[0]}<button
								type="button"
								class="preview-row"
								onclick={() => onopen(done[0].id)}
							>
								{@render line(
									done[0].state === 'error'
										? 'info'
										: done[0].state === 'interrupted'
											? 'stop'
											: 'check',
									done[0].title,
									t('agent.home.done') + ' · ' + formatDateTime(new Date(done[0].updated)),
									done[0].state === 'error' ? 'error' : 'plain',
								)}
							</button>{/if}
						{#if !next.length && !done.length && !running.length}
							<button type="button" class="preview-row" onclick={() => onshow('planned')}>
								{@render line('clock', t('agent.planned'), t('agent.home.planLink130'))}
							</button>
						{/if}
					</div>
				{:else}<div class="full-overview">
						{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
						{#if items.length}
							<SettingGroup title={t('agent.home.waiting')}>
								{#each items as item (item.id)}
									<div class="srow">
										<div class="srow-head">
											<span class="srow-ico"
												><Icon name={inboxIcon[item.kind] ?? 'bell'} size={19} /></span
											>
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

						<div class="stack">
							{#if rows.length && nas}
								<SettingGroup title={t('agent.home.state')}>
									{#snippet tools()}<span class="when"
											>{t('agent.home.asOf', { time: formatTime(new Date(nas!.at)) })}</span
										>{/snippet}
									{#each rows as r (r.id)}
										<a class="srow" href={r.href}>
											<div class="srow-head">
												<span
													class="srow-ico {r.tone === 'warn' || r.tone === 'error' ? r.tone : ''}"
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
											{#snippet tools()}<button
													type="button"
													class="more"
													onclick={() => onshow('planned')}
													>{t('agent.home.all', { count: activeSchedules(planned) })}</button
												>{/snippet}
											{#each next as s (s.id)}
												<button type="button" class="srow" onclick={() => onshow('planned')}>
													<div class="srow-head">
														<span class="srow-ico"
															><Icon
																name={s.kind === 'reminder' ? 'bell' : 'clock'}
																size={19}
															/></span
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
												{@const failed = chat.state === 'error'}
												{@const interrupted = chat.state === 'interrupted'}
												<button type="button" class="srow" onclick={() => onopen(chat.id)}>
													<div class="srow-head">
														<span
															class="srow-ico"
															class:error={failed}
															class:warn={interrupted}
															class:ok={chat.state === 'idle'}
														>
															<Icon
																name={failed ? 'info' : interrupted ? 'stop' : 'check'}
																size={19}
															/>
														</span>
														<div class="srow-text">
															<span class="srow-title">{chat.title}</span>
															<p class="srow-desc">
																{t(
																	failed
																		? 'tasks.state.failed'
																		: interrupted
																			? 'disktest.result.interrupted'
																			: 'tasks.state.done',
																)} · {formatDateTime(new Date(chat.updated))}{chat.preview
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
					</div>{/if}
			</div>
		</section>
	</div>
</PageShell>

<style>
	.home-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr));
		align-items: start;
		gap: var(--space-4);
	}
	.preview {
		min-width: 0;
	}
	.preview header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 44px;
	}
	.preview h2 {
		font-size: 1.06em;
		line-height: 1.25;
		margin: 0;
	}
	.header-action {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 5px;
		min-height: 44px;
		min-width: 44px;
		border: 0;
		border-radius: 8px;
		background: transparent;
		color: var(--ag-link, var(--accent));
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.preview-list {
		border: 1px solid color-mix(in srgb, var(--text) 15%, var(--surface));
		background: var(--ag-glass, var(--surface));
		border-radius: 14px;
		overflow: hidden;
	}
	.preview-row {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 48px;
		width: 100%;
		box-sizing: border-box;
		padding: 7px 12px;
		background: transparent;
		border: 0;
		color: var(--text);
		text-decoration: none;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.preview-row + .preview-row {
		border-top: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
	}
	.symbol,
	.line-end {
		flex: none;
		display: grid;
		place-items: center;
	}
	.symbol {
		color: var(--ag-link, var(--accent));
	}
	.symbol.attention {
		color: var(--warn-text, var(--status-warn));
	}
	.line-end {
		color: var(--muted);
	}
	.line-text {
		display: grid;
		gap: 2px;
		flex: 1;
		min-width: 0;
		line-height: 1.22;
		overflow-wrap: anywhere;
	}
	.line-text strong {
		font-size: 0.93em;
		font-weight: 550;
	}
	.line-text small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.preview-row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -3px;
	}
	.header-action:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	@media (hover: hover) {
		.preview-row:hover,
		.header-action:hover {
			background: color-mix(in srgb, var(--accent) 8%, transparent);
		}
	}
	.full-overview,
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
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
		color: var(--ag-link, var(--accent));
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
	@media (max-width: 760px) {
		.home-grid {
			grid-template-columns: 1fr;
			gap: var(--space-2);
		}
	}
</style>
