<script lang="ts">
	// My account › WebDAV: the password apps use for the shares over WebDAV,
	// made by the NAS and shown once.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type State = { enabled: boolean; set: boolean; created?: string };
	let info = $state<State | null>(null);
	let shown = $state<{ user: string; password: string } | null>(null);
	let removing = $state(false);
	let busy = $state(false);
	let error = $state('');

	async function load() {
		try {
			info = await api.get<State>('/account/webdav');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	onMount(load);

	async function create() {
		busy = true;
		error = '';
		try {
			shown = await api.post<{ user: string; password: string }>('/account/webdav', {});
			await load();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function remove() {
		busy = true;
		try {
			await api.delete('/account/webdav');
			removing = false;
			await load();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
</script>

{#if info?.enabled || info?.set}
	<section class="tile" id="webdav" aria-labelledby="acc-webdav">
		<h2 class="tile-title" id="acc-webdav">WebDAV</h2>
		<p class="muted">{t('webdav.accountIntro')}</p>
		<p class="muted small">{t('webdav.address')} <span class="mono">{location.origin}/dav/</span></p>
		{#if info.set && info.created}<p class="muted small">{t('webdav.since', { time: formatDateTime(new Date(info.created)) })}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button type="button" class="btn" disabled={busy || !info.enabled} onclick={create}>{t(info.set ? 'webdav.renew' : 'webdav.create')}</button>
			{#if info.set}<button type="button" class="btn" disabled={busy} onclick={() => (removing = true)}>{t('webdav.remove')}</button>{/if}
		</div>
	</section>
{/if}

{#if shown}
	<Dialog title={t('webdav.createdTitle')} onclose={() => (shown = null)}>
		<p>{t('webdav.createdHint')}</p>
		<dl class="grid">
			<dt>{t('webdav.server')}</dt><dd class="mono">{location.origin}/dav/</dd>
			<dt>{t('webdav.user')}</dt><dd class="mono">{shown.user}</dd>
			<dt>{t('webdav.password')}</dt><dd class="mono secret">{shown.password}</dd>
		</dl>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => shown && navigator.clipboard?.writeText(shown.password)}>{t('webdav.copy')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (shown = null)}>{t('wg.done')}</button>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('webdav.remove')} onclose={() => (removing = false)}>
		<p>{t('webdav.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}>{t('webdav.remove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.grid {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 4px 12px;
		margin: 8px 0;
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.secret {
		font-weight: 600;
	}
</style>
