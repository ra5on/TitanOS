<script lang="ts">
	// My account › WebDAV: the password apps use for the shares over WebDAV,
	// made by the NAS and shown once. One row of "Mein Konto" (PLAN 78-5).
	import { onMount, tick } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	type State = { enabled: boolean; set: boolean; created?: string };
	let info = $state<State | null>(null);
	let shown = $state<{ user: string; password: string } | null>(null);
	let removing = $state(false);
	// The NAS asks for the password (and a code) again, as for an API token.
	let asking = $state(false);
	let password = $state('');
	let code = $state('');
	let askError = $state('');
	let busy = $state(false);
	let error = $state('');

	async function load() {
		try {
			info = await api.get<State>('/account/webdav');
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	// The row appears only once the answer is there: a link to it
	// (/account#webdav) is followed then.
	let anchor = $state<HTMLElement>();
	onMount(async () => {
		await load();
		if (location.hash !== '#webdav') return;
		await tick();
		anchor?.scrollIntoView({ block: 'center' });
	});

	function ask() {
		password = '';
		code = '';
		askError = '';
		asking = true;
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		askError = '';
		try {
			shown = await api.post<{ user: string; password: string }>('/account/webdav', {
				password,
				code: code.trim(),
			});
			asking = false;
			password = '';
			code = '';
			await load();
		} catch (err) {
			askError = apiErrorText(err);
			// A used or wrong code cannot be sent again.
			if (err instanceof ApiError && err.code === 'invalid_code') code = '';
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
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if info?.enabled || info?.set}
	<SettingRow id="acc-webdav" icon="globe" title="WebDAV">
		{#snippet description()}
			<!-- The anchor of links from the WebDAV settings (/account#webdav). -->
			<span id="webdav" bind:this={anchor}>{t('webdav.accountIntro')}</span>
			<span class="line"
				>{t('webdav.address')} <span class="mono">{location.origin}/dav/</span></span
			>
			{#if info?.set && info.created}<span class="line"
					>{t('webdav.since', { time: formatDateTime(new Date(info?.created ?? '')) })}</span
				>{/if}
			{#if error}<span class="notice-error line" role="alert">{error}</span>{/if}
		{/snippet}
		{#snippet control()}
			<button type="button" class="btn" disabled={busy || !info?.enabled} onclick={ask}
				>{t(info?.set ? 'webdav.renew' : 'webdav.create')}</button
			>
			{#if info?.set}<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => (removing = true)}>{t('webdav.remove')}</button
				>{/if}
		{/snippet}
	</SettingRow>
{/if}

{#if asking}
	<Dialog title={t(info?.set ? 'webdav.renew' : 'webdav.create')} onclose={() => (asking = false)}>
		<form class="ask" onsubmit={create}>
			<ReconfirmFields bind:password bind:code />
			{#if askError}<p class="notice-error" role="alert">{askError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={busy || !reconfirmComplete(password, code)}
					>{t(info?.set ? 'webdav.renew' : 'webdav.create')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if shown}
	<Dialog title={t('webdav.createdTitle')} onclose={() => (shown = null)}>
		<p>{t('webdav.createdHint')}</p>
		<dl class="grid">
			<dt>{t('webdav.server')}</dt>
			<dd class="mono">{location.origin}/dav/</dd>
			<dt>{t('webdav.user')}</dt>
			<dd class="mono">{shown.user}</dd>
			<dt>{t('webdav.password')}</dt>
			<dd class="mono secret">{shown.password}</dd>
		</dl>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn"
				onclick={() => shown && navigator.clipboard?.writeText(shown.password)}
				>{t('webdav.copy')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (shown = null)}
				>{t('wg.done')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('webdav.remove')} onclose={() => (removing = false)}>
		<p>{t('webdav.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{t('webdav.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.line {
		display: block;
		margin-top: var(--space-1);
	}
	.ask {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.grid {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
		margin: var(--space-2) 0;
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.secret {
		font-weight: 600;
	}
</style>
