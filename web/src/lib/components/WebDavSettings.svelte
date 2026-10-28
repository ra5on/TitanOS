<script lang="ts">
	// Settings › Access › WebDAV: the switch for the whole NAS.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	let enabled = $state<boolean | null>(null);
	let busy = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			enabled = (await api.get<{ enabled: boolean }>('/system/webdav')).enabled;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	});

	async function toggle(next: boolean) {
		busy = true;
		error = '';
		try {
			enabled = (await api.put<{ enabled: boolean }>('/system/webdav', { enabled: next })).enabled;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" id="access-webdav" aria-labelledby="access-webdav-title">
	<h2 class="tile-title" id="access-webdav-title">WebDAV</h2>
	<p class="muted">{t('webdav.intro')}</p>
	{#if enabled !== null}
		<label class="check"><input type="checkbox" checked={enabled} disabled={busy} onchange={(e) => toggle(e.currentTarget.checked)} /><span>{t('webdav.enabled')}</span></label>
		{#if enabled}<p class="muted small">{t('webdav.address')} <span class="mono">{location.origin}/dav/</span> · <a href="/account#webdav">{t('webdav.passwordLink')}</a></p>{/if}
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>
