<script lang="ts">
	// On a new stick: put a saved configuration back instead of setting
	// the NAS up again. Shown on the setup page.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Found = {
		name: string;
		device: string;
		label: string;
		time: string;
		hostname: string;
		version: string;
		newer?: boolean;
		encrypted?: boolean;
	};
	type Source = { kind: 'found'; backup: Found } | { kind: 'file'; file: File };

	let found = $state<Found[]>([]);
	let chosen = $state(0);
	let source = $state<Source | null>(null);
	let phase = $state<'idle' | 'working' | 'restarting'>('idle');
	let error = $state('');
	// Encrypted backups need their password (decided 31.10.2026).
	let password = $state('');
	let sealedFile = $state(false);
	let picker = $state<HTMLInputElement>();

	// Looking at the disks takes a moment; the setup form does not wait for it.
	// Right after the start, mdadm may still be assembling an array (a
	// degraded one only after a while), so an empty answer is asked again
	// for a few minutes before it counts.
	let searching = $state(true);
	let searchError = $state('');
	const searchFor = 180_000;
	$effect(() => {
		const started = Date.now();
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;
		async function look() {
			try {
				const res = await api.get<{ backups: Found[] }>('/setup/backups');
				found = res.backups.slice(0, 10);
				searchError = '';
			} catch (err) {
				searchError = apiErrorText(err);
			}
			if (stopped) return;
			if (found.length === 0 && Date.now() - started < searchFor) timer = setTimeout(look, 10_000);
			else searching = false;
		}
		look();
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});

	function entry(backup: Found): string {
		return t('restore.entry', {
			time: formatDateTime(new Date(backup.time)),
			hostname: backup.hostname,
			version: backup.version,
		});
	}

	async function pick(event: Event) {
		const file = (event.currentTarget as HTMLInputElement).files?.[0];
		if (!file) return;
		// An encrypted backup starts with its own mark.
		sealedFile = (await file.slice(0, 13).text()) === 'TITANOS-ENC1\n';
		source = { kind: 'file', file };
	}

	function cancel() {
		source = null;
		password = '';
		if (picker) picker.value = '';
	}

	async function restore() {
		if (!source) return;
		error = '';
		phase = 'working';
		try {
			if (source.kind === 'found')
				await api.post('/setup/restore', {
					device: source.backup.device,
					name: source.backup.name,
					password: password || undefined,
				});
			else if (!password) await api.post('/setup/restore/upload', source.file);
			else {
				// The password goes in a header: the body is the file itself.
				const res = await fetch('/api/v1/setup/restore/upload', {
					method: 'POST',
					credentials: 'same-origin',
					headers: { 'X-Backup-Password': password },
					body: source.file,
				});
				if (!res.ok) {
					const body = await res.json().catch(() => ({}));
					throw new ApiError(
						res.status,
						body?.error?.code ?? 'internal',
						body?.error?.message ?? '',
					);
				}
			}
		} catch (err) {
			error = apiErrorText(err);
			phase = 'idle';
			// A wrong or missing password: try again in the same dialog.
			if (err instanceof ApiError && err.code.startsWith('backup_password')) return;
			cancel();
			return;
		}
		source = null;
		phase = 'restarting';
		waitForRestart();
	}

	// The NAS goes down and comes back with the accounts of the backup.
	// A request that gets no answer counts as "down": while a machine
	// restarts, connections tend to hang instead of failing.
	let slow = $state(false);
	function waitForRestart() {
		let wentDown = false;
		const started = Date.now();
		const timer = setInterval(async () => {
			slow = Date.now() - started > 120_000;
			try {
				const res = await fetch('/api/v1/session', {
					credentials: 'same-origin',
					signal: AbortSignal.timeout(2500),
				});
				if (res.ok && wentDown) {
					clearInterval(timer);
					location.reload();
				}
			} catch {
				wentDown = true;
			}
		}, 3000);
	}
</script>

{#if phase === 'restarting'}
	<div class="box" role="status">
		<p class="name">{t('restore.restarting')}</p>
		<p class="muted">{t('restore.restartingHint')}</p>
		{#if slow}
			<p class="notice-warn">{t('restore.slow')}</p>
			<div>
				<button type="button" class="btn" onclick={() => location.reload()}
					>{t('restore.reload')}</button
				>
			</div>
		{/if}
	</div>
{:else}
	{#if found.length > 0}
		<div class="box">
			<p class="name">{t('restore.found')}</p>
			<p class="muted">{t('restore.foundText')}</p>
			{#if found.length > 1}
				<label
					><span class="muted">{t('restore.choose')}</span>
					<select class="field" bind:value={chosen}>
						{#each found as backup, index (backup.device + backup.name)}<option value={index}
								>{entry(backup)}</option
							>{/each}
					</select>
				</label>
			{:else}
				<p>{entry(found[0])}</p>
			{/if}
			{#if found[chosen]?.newer}<p class="notice-warn" role="alert">
					{t('restore.newer', { version: found[chosen].version })}
				</p>{/if}
			<button
				type="button"
				class="btn btn-primary"
				disabled={phase !== 'idle'}
				onclick={() => (source = { kind: 'found', backup: found[chosen] })}
				>{t('restore.button')}</button
			>
		</div>
	{/if}
	{#if found.length === 0 && searching}<p class="muted" role="status">
			{t('restore.searching')}
		</p>{/if}
	{#if found.length === 0 && searchError}<p class="notice-warn">
			{t('restore.searchFailed', { error: searchError })}
		</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<details>
		<summary>{t('restore.fromFile')}</summary>
		<p class="muted">{t('restore.fileHint')}</p>
		<input
			bind:this={picker}
			type="file"
			accept=".gz,.tgz,application/gzip"
			onchange={pick}
			disabled={phase !== 'idle'}
		/>
	</details>
	{#if found.length > 0}<p class="muted or">{t('restore.or')}</p>{/if}
{/if}

{#if source}
	<Dialog title={t('restore.confirmTitle')} locked={phase === 'working'} onclose={cancel}>
		<p class="mono what">{source.kind === 'found' ? entry(source.backup) : source.file.name}</p>
		<p>{t('restore.confirmText')}</p>
		{#if source.kind === 'found' && source.backup.newer}<p class="notice-warn">
				{t('restore.newer', { version: source.backup.version })}
			</p>{/if}
		<p class="notice-warn">{t('restore.confirmAddress')}</p>
		{#if (source.kind === 'found' && source.backup.encrypted) || (source.kind === 'file' && sealedFile)}
			<label class="stack"
				><span>{t('restore.password')}</span><input
					class="field"
					type="password"
					autocomplete="off"
					bind:value={password}
				/></label
			>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if phase === 'working'}<p class="muted" role="status">{t('restore.working')}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={cancel} disabled={phase === 'working'}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={restore} disabled={phase === 'working'}
				>{t('restore.button')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.box {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px 16px;
		background: color-mix(in srgb, var(--accent) 10%, transparent);
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-ctl);
		font-size: var(--text-md);
	}
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	.name {
		font-size: var(--text-md);
		font-weight: 600;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	details {
		font-size: var(--text-base);
	}
	summary {
		cursor: pointer;
		color: var(--muted);
	}
	details p {
		margin: 8px 0;
	}
	details input {
		max-width: 100%;
		font: inherit;
		color: var(--text);
	}
	.or {
		font-size: var(--text-md);
		text-align: center;
	}
	.what {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
</style>
