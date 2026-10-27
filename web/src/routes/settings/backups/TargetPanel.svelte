<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Source = { name: string; created: string; bytes: number; last: string | null };
	type Target = { enabled: boolean; volume: string; port: number; sources: Source[]; listening: string[]; problem: string };
	type Overview = { target: Target; volumes: string[] | null };

	let overview = $state<Overview | null>(null);
	let error = $state('');
	// A failed look is its own message: the next one that works clears it,
	// without hiding what went wrong with a change.
	let loadError = $state('');
	let removeError = $state('');
	let busy = $state(false);
	let open = $state(false);
	let volume = $state('');
	let name = $state('');
	let pairing = $state<{ name: string; code: string } | null>(null);
	let removing = $state<Source | null>(null);
	let deleteData = $state(false);
	let copied = $state(false);

	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	async function load() {
		try {
			overview = await api.get<Overview>('/backups/target');
			volume ||= overview.target.volume || overview.volumes?.[0] || '';
			loadError = '';
		} catch (err) {
			loadError = fail(err);
		}
	}

	// Looked at once, then again only while the panel is open.
	$effect(() => {
		load();
	});
	$effect(() => {
		if (!open) return;
		load();
		const timer = setInterval(() => !document.hidden && load(), 20_000);
		return () => clearInterval(timer);
	});

	async function configure(enabled: boolean, box: HTMLInputElement) {
		busy = true;
		error = '';
		try {
			overview = await api.put<Overview>('/backups/target', { enabled, volume });
		} catch (err) {
			error = fail(err);
			// The setting did not change; neither does the box.
			box.checked = !enabled;
		}
		busy = false;
	}

	async function pair(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			const res = await api.post<{ code: string }>('/backups/target/sources', { name: name.trim() });
			pairing = { name: name.trim(), code: res.code };
			name = '';
			copied = false;
			await load();
		} catch (err) {
			error = fail(err);
		}
		busy = false;
	}

	async function remove() {
		if (!removing || busy) return;
		busy = true;
		removeError = '';
		try {
			overview = await api.delete<Overview>(`/backups/target/sources/${encodeURIComponent(removing.name)}${deleteData ? '?deleteData=true' : ''}`);
			removing = null;
		} catch (err) {
			removeError = fail(err);
		}
		busy = false;
	}

	async function copy() {
		if (!pairing) return;
		try {
			await navigator.clipboard.writeText(pairing.code);
			copied = true;
		} catch {
			// The code is shown and can be selected by hand.
		}
	}
</script>

<section class="tile panel target" aria-labelledby="backup-target">
	<div class="head">
		<h2 class="tile-title" id="backup-target">{t('backup.target')}</h2>
		<button type="button" class="btn small" aria-expanded={open} onclick={() => (open = !open)}>{open ? t('backup.targetHide') : t('backup.targetShow')}</button>
	</div>
	<p class="muted">{t('backup.targetDesc')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if loadError}<p class="notice-error" role="alert">{loadError} <button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button></p>{/if}
	{#if open && overview}
		{@const target = overview.target}
		<div class="row">
			<label class="check"><input type="checkbox" checked={target.enabled} disabled={busy || !volume} onchange={(event) => configure(event.currentTarget.checked, event.currentTarget)} /><span>{t('backup.targetOn')}</span></label>
			<label class="stack narrow"><span>{t('backup.targetVolume')}</span>
				<select class="field" bind:value={volume} disabled={target.enabled || busy}>
					{#each overview.volumes ?? [] as name (name)}<option value={name}>{name}</option>{/each}
				</select>
			</label>
		</div>
		{#if !overview.volumes?.length}<p class="notice-warn">{t('backup.targetNoVolume')}</p>{/if}
		{#if target.enabled}
			{#if target.problem}
				<p class="notice-warn">{t(`backup.targetProblem.${target.problem}` as 'backup.targetProblem.tailscale')}</p>
			{:else if target.listening.length > 0}
				<p class="muted">{t('backup.targetListening')} <span class="mono">{target.listening.join(', ')}</span></p>
			{/if}
		{/if}
		<h3 class="divided">{t('backup.targetSources')}</h3>
		{#each target.sources as source (source.name)}
			<div class="source">
				<span class="mono name">{source.name}</span>
				<span class="muted">{source.last ? t('backup.targetLast', { time: formatDateTime(new Date(source.last)) }) : t('backup.targetNever')} · {formatBytes(source.bytes)}</span>
				<button type="button" class="btn small" aria-label={t('backup.removeNamed', { name: source.name })} onclick={() => ((removing = source), (deleteData = false), (removeError = ''))}>{t('backup.targetRemove')}</button>
			</div>
		{:else}
			<p class="muted">{t('backup.targetNoSources')}</p>
		{/each}
		<form class="row" onsubmit={pair}>
			<label class="stack narrow"><span>{t('backup.targetName')}</span><input class="field mono" bind:value={name} pattern="[A-Za-z][A-Za-z0-9\-]*" maxlength="31" placeholder="keller" autocapitalize="none" spellcheck="false" required /></label>
			<button type="submit" class="btn" disabled={busy || !target.enabled || !!target.problem}>{t('backup.targetPair')}</button>
		</form>
		<p class="muted">{t('backup.targetPairHint')}</p>
	{/if}
</section>

{#if pairing}
	<Dialog title={t('backup.targetCodeTitle', { name: pairing.name })} onclose={() => (pairing = null)}>
		<p>{t('backup.targetCodeText')}</p>
		<textarea class="field mono" rows="5" readonly onfocus={(event) => event.currentTarget.select()}>{pairing.code}</textarea>
		<p class="notice-warn">{t('backup.targetCodeOnce')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={copy}>{copied ? t('backup.targetCopied') : t('backup.targetCopy')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (pairing = null)}>{t('backup.targetDone')}</button>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('backup.targetRemoveTitle', { name: removing.name })} locked={busy} onclose={() => (removing = null)}>
		<p>{t('backup.targetRemoveText')}</p>
		<label class="check"><input type="checkbox" bind:checked={deleteData} /><span>{t('backup.targetDeleteData')}</span></label>
		{#if deleteData}<p class="notice-error">{t('backup.targetDeleteWarn', { size: formatBytes(removing.bytes) })}</p>{/if}
		{#if removeError}<p class="notice-error" role="alert">{removeError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn {deleteData ? 'btn-danger' : 'btn-warn'}" disabled={busy} onclick={remove}>{t('backup.targetRemove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.target { overflow-wrap: anywhere; }
	.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 1rem; }
	.row { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 0.8rem 1.2rem; }
	.source { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 1rem; padding: 0.4rem 0; border-top: 1px solid var(--border); }
	.source .name { min-width: 8rem; }
	.narrow { width: min(100%, 20rem); }
	.narrow .field { width: 100%; min-width: 0; }
	.check { display: flex; align-items: center; gap: 0.5rem; }
	.check input { flex-shrink: 0; }
	h3 { margin: 0; font-size: 0.875rem; }
	textarea { width: 100%; box-sizing: border-box; }
</style>
