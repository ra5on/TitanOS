<script lang="ts">
	// Settings › Access › WebDAV: the switch for the whole NAS.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { page } from '$app/state';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let enabled = $state<boolean | null>(null);
	let busy = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			enabled = (await api.get<{ enabled: boolean }>('/system/webdav')).enabled;
		} catch (err) {
			error = apiErrorText(err);
		}
	});

	// A link to this row (#access-webdav): the rows above it arrive after the
	// browser's own anchor scroll, so bring it into view once it has its state.
	$effect(() => {
		if (enabled === null || page.url.hash !== '#access-webdav') return;
		requestAnimationFrame(() =>
			document.getElementById('access-webdav')?.scrollIntoView({ block: 'start' }),
		);
	});

	async function toggle(next: boolean) {
		busy = true;
		error = '';
		try {
			enabled = (await api.put<{ enabled: boolean }>('/system/webdav', { enabled: next })).enabled;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: one row of the group "Fernzugriff"; the switch stores on change as before. -->
<SettingRow id="access-webdav" icon="folder" title={t('webdav.enabled')} for="access-webdav-switch">
	{#snippet description()}
		{t('webdav.intro')}
		{#if enabled}<span class="address">
				{t('webdav.address')} <span class="mono">{location.origin}/dav/</span> ·
				<a href="/account#webdav">{t('webdav.passwordLink')}</a>
			</span>{/if}
	{/snippet}
	{#snippet control()}
		{#if enabled !== null}
			<input
				id="access-webdav-switch"
				class="switch"
				type="checkbox"
				role="switch"
				checked={enabled}
				disabled={busy}
				onchange={(e) => toggle(e.currentTarget.checked)}
			/>
		{/if}
	{/snippet}
</SettingRow>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<style>
	.address {
		display: block;
		margin-top: var(--space-1);
	}
</style>
