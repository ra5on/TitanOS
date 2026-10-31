<script lang="ts">
	// On a new stick: put a saved configuration back instead of setting
	// the NAS up again. Shown on the setup page around the account form
	// (children): a backup found on the disks comes first, the way back
	// from a file last.
	import type { Snippet } from 'svelte';
	import { api, apiFetch, ApiError } from '#lib/api.ts';
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

	let { children }: { children: Snippet } = $props();

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
	let searchRound = $state(0);
	const searchFor = 180_000;
	$effect(() => {
		void searchRound;
		searching = true;
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
			if (found.length === 0 && !searchError && Date.now() - started < searchFor)
				timer = setTimeout(look, 10_000);
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

	const sealed = $derived(
		(source?.kind === 'found' && !!source.backup.encrypted) ||
			(source?.kind === 'file' && sealedFile),
	);

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

	async function restore(event: SubmitEvent) {
		event.preventDefault();
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
				const res = await apiFetch('/setup/restore/upload', {
					method: 'POST',
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
				const res = await apiFetch('/session', {
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
		<section class="box" aria-labelledby="restore-found">
			<h2 class="name" id="restore-found">{t('restore.found')}</h2>
			<p class="muted">{t('restore.foundText')}</p>
			{#if found.length > 1}
				<fieldset>
					<legend class="muted">{t('restore.choose')}</legend>
					{#each found as backup, index (backup.device + backup.name)}
						<label class="choice">
							<input type="radio" name="restore-backup" value={index} bind:group={chosen} />
							<span>{entry(backup)}</span>
							{#if backup.encrypted}<span class="chip">{t('restore.encrypted')}</span>{/if}
						</label>
					{/each}
				</fieldset>
			{:else}
				<p class="single">
					<span>{entry(found[0])}</span>
					{#if found[0].encrypted}<span class="chip">{t('restore.encrypted')}</span>{/if}
				</p>
			{/if}
			{#if found[chosen]?.newer}<p class="notice-warn">
					{t('restore.newer', { version: found[chosen].version })}
				</p>{/if}
			<button
				type="button"
				class="btn btn-primary"
				disabled={phase !== 'idle'}
				onclick={() => (source = { kind: 'found', backup: found[chosen] })}
				>{t('restore.button')}</button
			>
		</section>
		<p class="or"><span>{t('restore.or')}</span></p>
	{/if}
	{#if error && !source}<p class="notice-error" role="alert">{error}</p>{/if}
	{@render children()}
	<section class="more" aria-labelledby="restore-more">
		<h2 id="restore-more">{t(found.length > 0 ? 'restore.moreFile' : 'restore.moreTitle')}</h2>
		{#if found.length === 0 && searching}<p class="muted searching" role="status">
				<span class="dot" aria-hidden="true"></span>{t('restore.searching')}
			</p>{/if}
		{#if found.length === 0 && searchError && !searching}<div class="notice-warn failed">
				<p>{t('restore.searchFailed', { error: searchError })}</p>
				<button type="button" class="btn small" onclick={() => searchRound++}
					>{t('restore.searchAgain')}</button
				>
			</div>{/if}
		<p class="muted">{t('restore.fileHint')}</p>
		<input
			bind:this={picker}
			class="sr-only"
			type="file"
			accept=".gz,.tgz,application/gzip"
			onchange={pick}
			disabled={phase !== 'idle'}
			tabindex="-1"
			aria-hidden="true"
		/>
		<div>
			<button type="button" class="btn" disabled={phase !== 'idle'} onclick={() => picker?.click()}
				>{t('restore.fromFile')}</button
			>
		</div>
	</section>
{/if}

{#if source}
	<Dialog title={t('restore.confirmTitle')} locked={phase === 'working'} onclose={cancel}>
		<form onsubmit={restore}>
			<p class="mono what">{source.kind === 'found' ? entry(source.backup) : source.file.name}</p>
			<p>{t('restore.confirmText')}</p>
			{#if source.kind === 'found' && source.backup.newer}<p class="notice-warn">
					{t('restore.newer', { version: source.backup.version })}
				</p>{/if}
			<p class="muted">{t('restore.confirmAddress')}</p>
			{#if sealed}
				<label class="stack"
					><span>{t('restore.password')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="field"
						type="password"
						autocomplete="off"
						bind:value={password}
						required
						autofocus
						aria-describedby="restore-password-hint"
					/><small class="muted" id="restore-password-hint">{t('restore.passwordHint')}</small
					></label
				>
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if phase === 'working'}<p class="muted" role="status">{t('restore.working')}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={cancel} disabled={phase === 'working'}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={phase === 'working'}
					>{t('restore.button')}</button
				>
			</div>
		</form>
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
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	legend {
		margin-bottom: 4px;
		padding: 0;
	}
	.choice {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.choice:has(input:checked) {
		border-color: var(--accent);
	}
	.choice span:first-of-type {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.single {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
		overflow-wrap: anywhere;
	}
	.box .btn-primary {
		align-self: flex-start;
		max-width: 100%;
	}
	.or {
		display: flex;
		align-items: center;
		gap: 12px;
		color: var(--muted);
		font-size: var(--text-base);
	}
	.or::before,
	.or::after {
		content: '';
		flex: 1;
		border-top: 1px solid var(--border);
	}
	.more {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding-top: 18px;
		border-top: 1px solid var(--border);
	}
	.more h2 {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	.more p {
		font-size: var(--text-base);
	}
	.failed {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 8px;
	}
	.searching {
		display: flex;
		align-items: baseline;
		gap: 8px;
	}
	.dot {
		flex: none;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		animation: pulse 1.4s ease-in-out infinite;
	}
	@keyframes pulse {
		50% {
			opacity: 0.3;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.dot {
			animation: none;
		}
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	.stack small {
		font-size: var(--text-sm);
	}
	.what {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
</style>
