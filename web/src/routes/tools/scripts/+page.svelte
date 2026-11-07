<script lang="ts">
	// Tools › Scripts: the person's own scripts, run as root by hand, on a
	// schedule, at start or at shutdown (package scripts in titand).
	import { api } from '#lib/api.ts';
	import { poll } from '#lib/poll.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
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

	{#if status}<section class="tile panel" aria-labelledby="scripts-place">
			<h2 class="tile-title" id="scripts-place">{t('scripts.place')}</h2>
			<form
				class="place"
				onsubmit={(e) => {
					e.preventDefault();
					void saveVolume();
				}}
			>
				<label class="stack"
					><span>{t('scripts.volume')}<HelpTip term="scriptsVolume" /></span><select
						class="field"
						bind:value={volume}
						disabled={busy}
						>{#if status.volume && !status.volumes.includes(status.volume)}<option
								value={status.volume}>{t('scripts.volumeAway', { volume: status.volume })}</option
							>{/if}{#each status.volumes as v (v)}<option value={v}>{v}</option>{/each}</select
					></label
				>
				<div class="row-actions">
					<button class="btn btn-primary" disabled={busy || !volumeChanged}>{t('ops.save')}</button>
				</div>
			</form>
			<p class="muted">
				{t('scripts.path')}
				<code class="mono">/mnt/{volume || status.volume || '…'}/.titanos/scripts</code>
				{#if !status.chosen && status.volume}· {t('scripts.defaultVolume')}{/if}
			</p>
		</section>

		<section class="tile panel" aria-labelledby="scripts-list">
			<h2 class="tile-title" id="scripts-list">{t('scripts.list')}</h2>
			<ul class="scripts">
				{#each status.scripts as script (script.id)}<li class="script">
						<div class="head">
							<div class="name">
								<strong>{script.name}</strong>
								{#if script.description}<span class="muted">{script.description}</span>{/if}
							</div>
							<div class="facts">
								<span class="chip">{when(script)}</span>
								{#if script.running}<span class="chip info" role="status"
										>{t('scripts.outcome.running')}</span
									>{:else if script.last}<span
										class="chip {outcomeTone(script.last.outcome)}"
										title={script.last.error}>{outcomeLabel(script.last)}</span
									>{/if}
							</div>
						</div>
						<p class="meta muted">{meta(script)}</p>
						<div class="row-actions">
							{#if script.running}<button class="btn" onclick={() => cancel(script)}
									>{t('scripts.cancel')}</button
								>{:else}<button
									class="btn btn-primary"
									disabled={status.disabled}
									onclick={() => start(script)}>{t('scripts.run')}</button
								>{/if}<button
								class="btn"
								aria-expanded={shown.includes(script.id)}
								onclick={() => toggleOutput(script.id)}
								>{t(shown.includes(script.id) ? 'scripts.hideOutput' : 'scripts.output')}</button
							><button class="btn" onclick={() => (history = script)}>{t('scripts.runs')}</button
							><button class="btn" disabled={script.running} onclick={() => (editing = script.id)}
								>{t('ops.edit')}</button
							>
						</div>
						{#if shown.includes(script.id)}<ScriptOutput
								id={script.id}
								name={script.name}
								running={script.running}
								onended={load}
							/>{/if}
					</li>{:else}<li>
						<EmptyState inline title={t('scripts.none')} />
					</li>{/each}
			</ul>
		</section>{/if}
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
		align-items: flex-end;
		gap: var(--space-3);
	}
	.place label {
		min-width: 14rem;
	}
	.scripts {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
	}
	.script {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3) 0;
		border-top: 1px solid var(--border);
	}
	.script:first-child {
		border-top: none;
		padding-top: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.name {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
		overflow-wrap: break-word;
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		align-items: flex-start;
	}
	.meta {
		margin: 0;
		font-size: var(--text-sm);
	}
</style>
