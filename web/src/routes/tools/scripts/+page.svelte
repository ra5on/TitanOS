<script lang="ts">
	// Tools › Scripts: the person's own scripts, run as root by hand, on a
	// schedule, at start or at shutdown (package scripts in titand).
	import { api } from '#lib/api.ts';
	import { poll } from '#lib/poll.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import ScriptEditor from './ScriptEditor.svelte';
	import ScriptOutput from './ScriptOutput.svelte';
	import ScriptRuns from './ScriptRuns.svelte';
	import {
		outcomeTone,
		runLabel,
		shortDuration,
		whenText,
		type ScriptRun,
		type ScriptView,
		type ScriptsStatus,
	} from './scripts.ts';

	let status = $state<ScriptsStatus | null>(null);
	let error = $state('');
	let actionError = $state('');
	let busy = $state(false);
	let volume = $state('');
	// The editor: null closed, '' a new script, else the id.
	let editing = $state<string | null>(null);
	// Scripts whose output is shown, and the one whose runs are open.
	let shown = $state<string[]>([]);
	let history = $state<ScriptView | null>(null);

	async function load() {
		try {
			status = await api.get<ScriptsStatus>('/scripts');
			if (!volume) volume = status.volume;
			error = '';
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	$effect(() => {
		void load();
	});
	// While a script runs, the list follows it (for runs from the schedule too).
	const anyRunning = $derived(!!status?.scripts.some((s) => s.running));
	$effect(() => {
		if (!anyRunning) return;
		return poll(load, { interval: 3000, immediate: false });
	});

	async function saveVolume() {
		busy = true;
		actionError = '';
		try {
			status = await api.put<ScriptsStatus>('/scripts/volume', { volume });
		} catch (err) {
			actionError = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function start(script: ScriptView) {
		actionError = '';
		try {
			await api.post(`/scripts/${script.id}/run`);
			if (!shown.includes(script.id)) shown = [...shown, script.id];
			await load();
		} catch (err) {
			actionError = `${script.name}: ${apiErrorText(err)}`;
		}
	}

	async function cancel(script: ScriptView) {
		actionError = '';
		try {
			await api.post(`/scripts/${script.id}/cancel`);
		} catch (err) {
			actionError = `${script.name}: ${apiErrorText(err)}`;
		}
	}

	function toggleOutput(id: string) {
		shown = shown.includes(id) ? shown.filter((s) => s !== id) : [...shown, id];
	}

	const join = (items: string[]) =>
		new Intl.ListFormat(i18n.locale, { type: 'conjunction' }).format(items);
	const when = (script: ScriptView) => whenText(script, tr, join);
	const time = (value?: string) =>
		value ? formatDateTime(new Date(value), status?.timezone || undefined) : '—';
	const tr = (key: string, values?: Record<string, string | number>) =>
		t(key as MessageKey, values);
	const outcomeLabel = (run: ScriptRun) => runLabel(run, tr);
	function meta(script: ScriptView) {
		const parts = [
			script.last
				? t('scripts.lastRun', {
						time: time(script.last.started),
						duration: shortDuration(script.last.durationMs),
					})
				: t('scripts.neverRun'),
		];
		if (script.next) parts.push(t('scripts.nextRun', { time: time(script.next) }));
		return parts.join(' · ');
	}
	const volumeChanged = $derived(!!status && volume !== '' && volume !== status.volume);

	// PLAN 67-1: the menu bar of the desktop – new script, run one, its runs.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const byId = (id?: string) => status?.scripts.find((s) => s.id === id);
	registerMenuCommands({
		'planner.newScript': { run: () => (editing = ''), enabled: () => !!status?.available },
		'planner.run': {
			items: () =>
				(status?.disabled ? [] : (status?.scripts ?? []))
					.filter((s) => !s.running)
					.map((s) => ({ id: s.id, text: s.name })),
			run: (id) => {
				const script = byId(id);
				if (script) void start(script);
			},
		},
		'planner.history': {
			items: () => (status?.scripts ?? []).map((s) => ({ id: s.id, text: s.name })),
			run: (id) => {
				const script = byId(id);
				if (script) history = script;
			},
		},
		'planner.refresh': { run: () => void load(), enabled: () => !busy },
	});
</script>

<PageHead title={t('scripts.title')}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		><button class="btn btn-primary" disabled={!status?.available} onclick={() => (editing = '')}
			>{t('scripts.add')}</button
		>{/snippet}</PageHead
>

<div class="column">
	<p class="muted">{t('scripts.hint')}</p>
	<p class="notice-warn">{t('scripts.root')}</p>
	{#if error}<p class="notice-error" role="alert">
			{error} <button class="btn small" onclick={load}>{t('ui.retry')}</button>
		</p>{/if}
	{#if status?.disabled}<p class="notice-warn">{t('scripts.devMode')}</p>{/if}
	{#if status && !status.available}<p class="notice-error" role="alert">
			{status.volume
				? t('scripts.volumeMissing', { volume: status.volume })
				: t('scripts.noVolume')}
		</p>{/if}
	{#if status?.error}<p class="notice-warn">{status.error}</p>{/if}
	{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}

	<!-- PLAN 78-5: where the scripts live is one flat row; the list of the
	     scripts stands under a plain heading. -->
	{#if status}<SettingGroup title={t('scripts.place')} id="scripts-place">
			<SettingRow icon="disk" title={t('scripts.volume')} for="scripts-volume">
				{#snippet badge()}<HelpTip term="scriptsVolume" />{/snippet}
				{#snippet description()}
					{t('scripts.path')}
					<code class="mono">/mnt/{volume || status?.volume || '…'}/.titanos/scripts</code>
					{#if !status?.chosen && status?.volume}· {t('scripts.defaultVolume')}{/if}
				{/snippet}
				{#snippet control()}
					<form
						class="place"
						onsubmit={(e) => {
							e.preventDefault();
							void saveVolume();
						}}
					>
						<select id="scripts-volume" class="field" bind:value={volume} disabled={busy}
							>{#if status?.volume && !status.volumes.includes(status.volume)}<option
									value={status.volume}>{t('scripts.volumeAway', { volume: status.volume })}</option
								>{/if}{#each status?.volumes ?? [] as v (v)}<option value={v}>{v}</option
								>{/each}</select
						>
						<button class="btn btn-primary" disabled={busy || !volumeChanged}
							>{t('ops.save')}</button
						>
					</form>
				{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('scripts.list')} id="scripts-list">
			<!-- Style W4 (0.74): one row per script, its output opens below it. -->
			{#if status.scripts.length}
				<div class="table-wrap">
					<table class="data-table scripts">
						<thead
							><tr
								><th>{t('scripts.col.name')}</th><th>{t('scripts.col.when')}</th><th
									>{t('scripts.col.last')}</th
								><th class="actions"><span class="sr-only">{t('scripts.col.actions')}</span></th
								></tr
							></thead
						>
						<tbody>
							{#each status.scripts as script (script.id)}
								<tr>
									<td class="name"
										><strong>{script.name}</strong>{#if script.description}<span class="muted small"
												>{script.description}</span
											>{/if}<span class="meta muted small">{meta(script)}</span></td
									>
									<td>{when(script)}</td>
									<td
										>{#if script.running}<span class="chip info" role="status"
												>{t('scripts.outcome.running')}</span
											>{:else if script.last}<span
												class="chip {outcomeTone(script.last.outcome)}"
												title={script.last.error}>{outcomeLabel(script.last)}</span
											>{:else}<span class="muted none">—</span>{/if}</td
									>
									<td class="actions"
										>{#if script.running}<button class="btn small" onclick={() => cancel(script)}
												>{t('scripts.cancel')}</button
											>{:else}<button
												class="btn btn-primary small"
												disabled={status.disabled}
												onclick={() => start(script)}>{t('scripts.run')}</button
											>{/if}<button
											class="btn small"
											aria-expanded={shown.includes(script.id)}
											onclick={() => toggleOutput(script.id)}
											>{t(
												shown.includes(script.id) ? 'scripts.hideOutput' : 'scripts.output',
											)}</button
										><button class="btn small" onclick={() => (history = script)}
											>{t('scripts.runs')}</button
										><button
											class="btn small"
											disabled={script.running}
											onclick={() => (editing = script.id)}>{t('ops.edit')}</button
										></td
									>
								</tr>
								{#if shown.includes(script.id)}<tr class="output"
										><td colspan="4"
											><ScriptOutput
												id={script.id}
												name={script.name}
												running={script.running}
												onended={load}
											/></td
										></tr
									>{/if}
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<EmptyState inline title={t('scripts.none')} />
			{/if}
		</SettingGroup>
		<StatusBar label={t('scripts.statusBar')}>
			<span>{t('scripts.statCount', { count: status.scripts.length })}</span>
			<span>{t('scripts.statRunning')} <b>{status.scripts.filter((x) => x.running).length}</b></span
			>
			{#if status.volume}<span class="end">{t('scripts.statPlace', { volume: status.volume })}</span
				>{/if}
		</StatusBar>{/if}
</div>

{#if editing !== null}<ScriptEditor
		id={editing}
		timezone={status?.timezone}
		onclose={() => (editing = null)}
		onsaved={() => {
			editing = null;
			void load();
		}}
	/>{/if}
{#if history}<ScriptRuns
		script={history}
		timezone={status?.timezone}
		onclose={() => (history = null)}
	/>{/if}

<style>
	.column {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		max-width: 64rem;
	}
	.column > p {
		margin: 0;
	}
	.place {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: inherit;
		gap: var(--space-2) var(--space-2-5);
		min-width: 0;
	}
	.meta {
		margin: 0;
		font-size: var(--text-sm);
	}
	.scripts .name strong,
	.scripts .name span {
		display: block;
	}
	.scripts td.actions {
		white-space: nowrap;
	}
	.scripts td.actions .btn {
		margin: 2px 0 2px var(--space-1);
	}
	.scripts tr.output td {
		padding-top: 0;
	}
	@media (max-width: 640px) {
		.scripts thead {
			display: none;
		}
		.scripts tr {
			display: grid;
			gap: var(--space-1);
			padding: var(--space-2) 0;
			border-bottom: 1px solid var(--border);
		}
		/* Without the table's head a lone dash says nothing (the row names "not run yet"). */
		.scripts td:has(> .none) {
			display: none;
		}
		/* No rule and no gap below the last script of the group. */
		.scripts tbody tr:last-child {
			padding-bottom: 0;
			border-bottom: 0;
		}
		.scripts td {
			padding: 0;
			border: 0;
		}
		.scripts td.actions {
			text-align: left;
			white-space: normal;
		}
		.scripts td.actions .btn {
			margin: 2px var(--space-1) 2px 0;
		}
	}
</style>
