<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	// LUKS2 encryption of a volume (internal/storage crypt.go): unlock with
	// the password after a start, lock, passwords and the header backup.
	// Plain confirmations; passwords stay only as long as a dialog is open.
	import { api, ApiError, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { refreshStorage } from '#lib/storage.svelte.ts';

	type Props = { volume: Volume; busy: boolean };
	let { volume, busy }: Props = $props();

	type Kind = 'unlock' | 'lock' | 'change' | 'add' | 'remove' | 'header';
	let dialog = $state<Kind | null>(null);
	let current = $state('');
	let password = $state('');
	let repeat = $state('');
	let working = $state(false);
	let error = $state('');

	const encryption = $derived(volume.encryption);
	const base = $derived(`/storage/volumes/${encodeURIComponent(volume.name)}`);
	const needsNew = $derived(dialog === 'change' || dialog === 'add');
	const problem = $derived.by(() => {
		if (!needsNew) return '';
		if (password.length > 0 && password.length < 8) return t('crypt.tooShort');
		if (repeat !== '' && repeat !== password) return t('crypt.mismatch');
		return '';
	});
	const ready = $derived.by(() => {
		if (working) return false;
		if (dialog === 'lock' || dialog === 'header') return true;
		if (!current) return false;
		return !needsNew || (password.length >= 8 && password === repeat);
	});

	function open(kind: Kind) {
		current = password = repeat = error = '';
		dialog = kind;
	}
	function close() {
		current = password = repeat = '';
		dialog = null;
	}

	function codeOf(err: unknown): string {
		return err instanceof ApiError ? err.code : 'internal';
	}

	async function downloadHeader() {
		const res = await fetch(apiPath(`${base}/luks-header`), { credentials: 'same-origin' });
		if (!res.ok) {
			let code = 'internal';
			try {
				code = (await res.json())?.error?.code ?? code;
			} catch {
				/* not JSON */
			}
			throw new ApiError(res.status, code, '');
		}
		const blob = await res.blob();
		const link = document.createElement('a');
		link.href = URL.createObjectURL(blob);
		link.download = `${volume.name}-luks-header.tar`;
		link.click();
		setTimeout(() => URL.revokeObjectURL(link.href), 10_000);
	}

	// Runs only from a click on its button (or Enter in the password field
	// of the unlock dialog, which destroys nothing).
	async function submit() {
		if (!ready || !dialog) return;
		working = true;
		error = '';
		try {
			if (dialog === 'unlock') await api.post(`${base}/unlock`, { password: current });
			else if (dialog === 'lock') await api.post(`${base}/lock`, {});
			else if (dialog === 'header') await downloadHeader();
			else await api.post(`${base}/passwords`, { action: dialog, current, password: needsNew ? password : undefined });
			close();
			refreshStorage();
		} catch (err) {
			const code = codeOf(err);
			error = errorText(code);
			if (code === 'volume_password_attempts' && err instanceof ApiError && err.retryAfter > 0) error = t('crypt.retry', { seconds: err.retryAfter });
			current = '';
		}
		working = false;
	}
</script>

{#if encryption}
	<div class="crypt">
		<span class="label">{t('crypt.title')}</span>
		<span>
			{#if encryption.locked}<span class="locked">{t('crypt.locked')}</span>
			{:else}{t('crypt.unlocked')}{/if}
			{#if encryption.members > 1} · {t('crypt.members', { open: encryption.unlocked, total: encryption.members })}{/if}
			{#if encryption.passwords > 0} · {t('crypt.passwords', { count: encryption.passwords })}{/if}
		</span>
		<span class="buttons">
			{#if encryption.locked || encryption.unlocked < encryption.members}
				<button type="button" class="btn small btn-primary" disabled={busy} onclick={() => open('unlock')}>{t('crypt.unlock')}</button>
			{/if}
			{#if !encryption.locked}
				<button type="button" class="btn small" disabled={busy} onclick={() => open('lock')}>{t('crypt.lock')}</button>
			{/if}
			<button type="button" class="btn small" disabled={busy} onclick={() => open('change')}>{t('crypt.change')}</button>
			<button type="button" class="btn small" disabled={busy} onclick={() => open('add')}>{t('crypt.add')}</button>
			{#if encryption.passwords > 1}<button type="button" class="btn small" disabled={busy} onclick={() => open('remove')}>{t('crypt.remove')}</button>{/if}
			<button type="button" class="btn small" disabled={busy} onclick={() => open('header')}>{t('crypt.header')}</button>
		</span>
		{#if encryption.locked}<p class="notice-warn">{t('crypt.lockedNote')}</p>{/if}
	</div>
{/if}

{#if dialog}
	<Dialog title={t(`crypt.${dialog}Title`, { volume: volume.name })} locked={working} onclose={close}>
		<form
			class="body"
			onsubmit={(event) => {
				event.preventDefault();
				if (dialog === 'unlock') submit();
			}}
		>
			<p>{t(`crypt.${dialog}Text`, { volume: volume.name })}</p>
			{#if dialog !== 'lock' && dialog !== 'header'}
				<label class="stack">
					<span>{t(dialog === 'unlock' ? 'crypt.password' : dialog === 'remove' ? 'crypt.removeWhich' : 'crypt.current')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field" type="password" autocomplete="off" bind:value={current} maxlength="512" autofocus />
				</label>
			{/if}
			{#if needsNew}
				<label class="stack">
					<span>{t('crypt.new')}</span>
					<input class="field" type="password" autocomplete="new-password" bind:value={password} maxlength="512" />
				</label>
				<label class="stack">
					<span>{t('crypt.repeat')}</span>
					<input class="field" type="password" autocomplete="new-password" bind:value={repeat} maxlength="512" />
					{#if problem}<small class="problem">{problem}</small>{/if}
				</label>
			{/if}
			{#if dialog === 'header'}<p class="notice-warn" role="alert">{t('crypt.headerWarning')}</p>{/if}
			{#if dialog === 'change'}<p class="muted">{t('crypt.changeHint')}</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={working} onclick={close}>{t('common.cancel')}</button>
				<button
					type={dialog === 'unlock' ? 'submit' : 'button'}
					class="btn {dialog === 'lock' || dialog === 'remove' ? 'btn-warn' : 'btn-primary'}"
					disabled={!ready}
					onclick={dialog === 'unlock' ? undefined : submit}>{t(working && dialog === 'lock' ? 'crypt.locking' : `crypt.${dialog}Submit`)}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	.crypt {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
		font-size: 0.875rem;
	}
	.label {
		font-weight: 600;
	}
	.locked {
		color: var(--warn-text);
		font-weight: 600;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-left: auto;
	}
	.crypt p {
		flex-basis: 100%;
		margin: 0;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.body p {
		margin: 0;
		font-size: 0.875rem;
	}
	.problem {
		color: var(--warn-text);
		font-size: 0.75rem;
	}
</style>
