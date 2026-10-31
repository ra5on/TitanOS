<script lang="ts">
	// "Download from link": titand fetches an HTTP(S) URL into the current
	// folder as a background job. The name comes from the URL path or, after
	// asking the server, from Content-Disposition; it stays editable. An
	// existing name is never overwritten: the person may keep both instead.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		downloadName,
		validDigest,
		validDownloadName,
		validDownloadURL,
	} from './url-download.ts';

	type Probe = { name: string; size: number; type?: string; exists: boolean; free: number };
	type Props = {
		root: string;
		dir: string;
		/** Accounts with the role "user" only reach public addresses. */ confined: boolean;
		onclose: () => void;
		onstarted: () => void;
	};
	let { root, dir, confined, onclose, onstarted }: Props = $props();

	let url = $state(''),
		name = $state(''),
		sha256 = $state(''),
		nameEdited = $state(false),
		busy = $state(false),
		probing = $state(false),
		error = $state(''),
		conflict = $state(false),
		probe = $state<Probe | null>(null),
		probeError = $state('');
	let probeRun = 0;

	const urlValid = $derived(validDownloadURL(url.trim()));
	const nameValid = $derived(validDownloadName(name.trim()));
	const digestValid = $derived(validDigest(sha256.trim()));
	const tooLarge = $derived(!!probe && probe.size > 0 && probe.free > 0 && probe.size > probe.free);

	function urlChanged() {
		probe = null;
		probeError = '';
		conflict = false;
		probeRun++;
		if (!nameEdited) name = downloadName(url.trim());
	}

	// Asking the link is optional: it fills the name the server suggests and
	// shows the size. Nothing is written.
	async function ask() {
		if (!urlValid || probing) return;
		const run = ++probeRun;
		probing = true;
		probeError = '';
		try {
			const result = await api.post<Probe>('/files/url-download/probe', {
				root,
				dir,
				url: url.trim(),
			});
			if (run !== probeRun) return;
			probe = result;
			if (!nameEdited && result.name) name = result.name;
		} catch (err) {
			if (run === probeRun) probeError = apiErrorText(err);
		} finally {
			if (run === probeRun) probing = false;
		}
	}

	async function start(keepBoth: boolean) {
		if (busy) return;
		if (!urlValid || !nameValid || !digestValid) {
			error = t(
				!urlValid ? 'files.urlInvalid' : !nameValid ? 'files.invalidPath' : 'files.sha256Invalid',
			);
			return;
		}
		busy = true;
		error = '';
		try {
			await api.post('/files/url-download', {
				root,
				dir,
				url: url.trim(),
				name: name.trim(),
				sha256: sha256.trim(),
				conflict: keepBoth ? 'rename' : '',
			});
			onstarted();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			conflict = code === 'file_exists';
			error = conflict ? '' : errorText(code);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('files.urlDownload')} {onclose} locked={busy}>
	<form
		onsubmit={(event) => {
			event.preventDefault();
			start(false);
		}}
	>
		<label
			>{t('files.urlDownloadURL')}<input
				class="field mono"
				type="url"
				inputmode="url"
				autocomplete="off"
				spellcheck="false"
				placeholder="https://"
				required
				maxlength="8192"
				bind:value={url}
				oninput={urlChanged}
				onblur={ask}
				disabled={busy}
				aria-invalid={url.trim() !== '' && !urlValid}
			/></label
		>
		{#if url.trim() !== '' && !urlValid}<p class="notice-warn small" role="status">
				{t('files.urlInvalid')}
			</p>{/if}
		<div class="line">
			<button type="button" class="btn small" disabled={!urlValid || probing || busy} onclick={ask}
				>{t(probing ? 'files.urlProbing' : 'files.urlProbe')}</button
			>
			{#if probe}<span class="muted small"
					>{probe.size >= 0
						? formatBytes(probe.size)
						: t('files.urlSizeUnknown')}{#if probe.type}{' · '}<span class="mono">{probe.type}</span
						>{/if}</span
				>{/if}
		</div>
		{#if probeError}<p class="notice-warn small" role="status">{probeError}</p>{/if}
		{#if tooLarge}<p class="notice-warn small" role="status">
				{t('files.urlTooLarge', { free: formatBytes(probe?.free ?? 0) })}
			</p>{/if}
		<label
			>{t('files.name')}<input
				class="field"
				required
				maxlength="255"
				bind:value={name}
				oninput={() => {
					nameEdited = true;
					conflict = false;
				}}
				disabled={busy}
				aria-invalid={name.trim() !== '' && !nameValid}
			/></label
		>
		{#if probe?.exists && probe.name === name.trim() && !conflict}<p class="muted small">
				{t('files.urlNameExists')}
			</p>{/if}
		<label
			>{t('files.urlSHA256')}<input
				class="field mono"
				autocomplete="off"
				spellcheck="false"
				maxlength="64"
				placeholder={t('files.urlOptional')}
				bind:value={sha256}
				disabled={busy}
				aria-invalid={!digestValid}
			/></label
		>
		<p class="muted small">
			{t('files.urlDownloadHint')}{#if confined}
				{t('files.urlDownloadPublicOnly')}{/if}
		</p>
		{#if conflict}<div class="notice-warn" role="alert">
				<p>{t('files.urlConflict', { name: name.trim() })}</p>
				<button type="button" class="btn" disabled={busy} onclick={() => start(true)}
					>{t('files.conflict.rename')}</button
				>
			</div>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			<button
				class="btn btn-primary"
				disabled={busy || !urlValid || !nameValid || !digestValid || conflict}
				>{t('files.urlDownloadStart')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: grid;
		gap: 0.7rem;
	}
	label {
		display: grid;
		gap: 0.3rem;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.6rem;
	}
	.small {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	.notice-warn p {
		margin: 0 0 0.5rem;
	}
</style>
