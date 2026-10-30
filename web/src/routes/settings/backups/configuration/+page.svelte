<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import { api, ApiError } from '#lib/api.ts';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';

	type Settings = { enabled: boolean; volume: string; keep: number; schedule: string; time: string; weekday: number };
	type Backup = { name: string; volume: string; time: string; sizeBytes: number; hostname: string; version: string };
	type State = { settings: Settings; target: string; lastCheck: string | null; lastError: string; backups: Backup[]; next: string | null };
	type Answer = { backup: State; volumes: string[] | null };

	let status = $state<State | null>(null);
	let volumes = $state<string[]>([]);
	let draft = $state<Settings | null>(null);
	let saved = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state<'save' | 'run' | null>(null);
	let ran = $state(false);
	// The backup that waits for the password to be entered again.
	let wanted = $state<Backup | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: code === 'backup_failed' && err instanceof ApiError ? err.message : '' };
	}

	function take(res: Answer, keepDraft = false) {
		status = res.backup;
		volumes = res.volumes ?? [];
		if (!draft || !keepDraft) draft = { ...res.backup.settings };
	}

	async function load() { error = null; try { take(await api.get<Answer>('/config-backup')); } catch (err) { problem(err); } }
	$effect(() => { load(); });
	$effect(() => { if (saved) { const timer = setTimeout(() => saved = false, 5000); return () => clearTimeout(timer); } });

	const changed = $derived(!!status && !!draft && JSON.stringify({ ...draft, keep: Number(draft.keep) }) !== JSON.stringify(status.settings));
	const newest = $derived(status?.backups[0]);

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = 'save'; saved = false;
		error = null;
		try {
			take(await api.put<Answer>('/config-backup', { ...draft, keep: Number(draft.keep) || 0 })); saved = true;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	async function run() {
		busy = 'run';
		error = null;
		ran = false;
		try {
			take(await api.post<Answer>('/config-backup/run'), true);
			ran = true;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	function fetchBackup(backup: Backup) {
		// The browser saves the file itself; nothing of it passes through the page.
		const link = document.createElement('a');
		link.href = apiPath(`/config-backup/download?volume=${encodeURIComponent(backup.volume)}&name=${encodeURIComponent(backup.name)}`);
		link.download = '';
		link.click();
	}

	async function download(backup: Backup) {
		error = null;
		try {
			if ((await api.get<{ confirmed: boolean }>('/session/confirmed')).confirmed) fetchBackup(backup);
			else wanted = backup;
		} catch (err) {
			problem(err);
		}
	}

	function confirmed() {
		if (wanted) fetchBackup(wanted);
		wanted = null;
	}


</script>

<PageHead title={t('safeguard.config')} parent={{ href: '/settings/backups', label: t('safeguard.title') }}>
	<p class="muted">{t('stick.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}{#if !status}<button class="btn" onclick={load}>{t('ui.retry')}</button>{/if}</div>{/if}
{#if !status && !error}<p class="loading" role="status">{t('common.loading')}</p>{/if}

{#if status && draft}
	<section class="tile panel" id="config-history" use:revealTarget aria-labelledby="stick-state">
		<h2 class="tile-title" id="stick-state">{t('stick.state')}</h2>
		{#if !status.settings.enabled}
			<p class="notice-warn">{t('stick.off')}</p>
		{:else if !status.target}
			<p class="notice-warn">{t(status.settings.volume ? 'stick.volumeMissing' : 'stick.noVolume', { volume: status.settings.volume })}</p>
		{:else if status.lastError}
			<div class="notice-error" role="alert">{t('stick.failed')}<pre>{status.lastError}</pre></div>
		{:else if newest}
			<p class="notice-ok" role="status">{t('stick.ok', { time: formatDateTime(new Date(newest.time)), volume: newest.volume })}</p>
		{:else}
			<p class="muted">{t('stick.none')}</p>
		{/if}
		<dl>
			<div><dt>{t('stick.target')}</dt><dd class="mono">{status.target ? `/mnt/${status.target}/.titanos/backups` : '–'}</dd></div>
			<div><dt>{t('stick.lastCheck')}</dt><dd>{status.lastCheck ? formatDateTime(new Date(status.lastCheck)) : '–'}</dd></div>
			{#if status.next}<div><dt>{t('stick.next')}</dt><dd>{formatDateTime(new Date(status.next))}</dd></div>{/if}
			<div><dt>{t('stick.count')}</dt><dd>{status.backups.length}</dd></div>
		</dl>
		<div class="row-actions">
			<button type="button" class="btn" disabled={busy !== null || !status.target} onclick={run}>{t(busy === 'run' ? 'stick.running' : 'stick.run')}</button>
			{#if ran}<span class="muted" role="status">{t('stick.ran')}</span>{/if}
		</div>
	</section>

	<form class="tile panel" id="config-schedule" use:revealTarget onsubmit={save} aria-labelledby="stick-settings">
		<h2 class="tile-title" id="stick-settings">{t('stick.settings')}</h2>
		<div class="row">
			<label class="stack narrow"><span>{t('stick.schedule')}</span><select class="field" bind:value={draft.schedule} onchange={() => { draft!.time ||= '03:00'; }}>
				<option value="">{t('stick.schedule.off')}</option><option value="daily">{t('stick.schedule.daily')}</option><option value="weekly">{t('stick.schedule.weekly')}</option></select></label>
			{#if draft.schedule}<label class="stack tiny"><span>{t('stick.schedule.time')}</span><input class="field" type="time" bind:value={draft.time} required /></label>{/if}
			{#if draft.schedule === 'weekly'}<label class="stack tiny"><span>{t('stick.schedule.day')}</span><select class="field" bind:value={draft.weekday}>{#each [1,2,3,4,5,6,0] as const as day}<option value={day}>{t(`backup.weekday.${day}`)}</option>{/each}</select></label>{/if}
		</div>
		<p class="muted">{t('stick.schedule.hint')}</p>
		<div class="option divided">
			<label class="check"><input type="checkbox" bind:checked={draft.enabled} /><span>{t('stick.enabled')}</span></label>
			<small>{t('stick.enabledHint')}</small>
		</div>
		<div class="row">
			<label class="stack narrow"><span>{t('stick.volume')}</span>
				<select class="field" bind:value={draft.volume}>
					<option value="">{t('stick.volumeAuto')}</option>
					{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
				</select>
			</label>
			<label class="stack tiny"><span>{t('stick.keep')}</span>
				<input class="field mono" type="number" min="1" max="100" bind:value={draft.keep} required />
			</label>
		</div>
		<div class="row-actions">
			<button type="submit" class="btn btn-primary" disabled={!changed || busy !== null}>{t(busy === 'save' ? 'ui.saving' : 'notify.save')}</button>{#if saved && !changed}<span class="muted" role="status">{t('ui.saved')}</span>{/if}
		</div>
	</form>

	<section class="tile panel" id="config-downloads" use:revealTarget aria-labelledby="stick-list">
		<h2 class="tile-title" id="stick-list">{t('stick.backups')}</h2>
		{#if status.backups.length === 0}
			<p class="muted">{t('stick.none')}</p>
		{:else}
			<p class="muted">{t('stick.downloadHint')}</p>
			<div class="scroll">
				<table>
					<thead><tr><th>{t('stick.when')}</th><th>{t('stick.volume')}</th><th>{t('stick.version')}</th><th class="num">{t('stick.size')}</th><th></th></tr></thead>
					<tbody>
						{#each status.backups as backup (backup.volume + backup.name)}
							<tr>
								<td>{formatDateTime(new Date(backup.time))}</td>
								<td class="mono">{backup.volume}</td>
								<td class="mono">{backup.version}</td>
								<td class="num mono">{formatBytes(backup.sizeBytes)}</td>
								<td class="num"><button type="button" class="btn small" onclick={() => download(backup)}>{t('stick.download')}</button></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

{/if}

{#if wanted}
	<ConfirmDialog reason={t('stick.confirmReason')} onconfirmed={confirmed} onclose={() => (wanted = null)} />
{/if}

<style>
	section,
	form {
		font-size: 0.875rem;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.narrow {
		flex: 0 1 320px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 160px;
		max-width: 160px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px 24px;
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.option small {
		padding-left: 28px;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.scroll {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		padding: 8px 12px 8px 0;
		text-align: left;
		border-bottom: 1px solid var(--panel-border);
		white-space: nowrap;
	}
	th {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--muted);
	}
	.num {
		text-align: right;
	}
	.small {
		height: 2rem;
		padding: 0 12px;
		font-size: 0.8125rem;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
