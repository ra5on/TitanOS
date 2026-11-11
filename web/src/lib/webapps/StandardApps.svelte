<script lang="ts">
	// PLAN 66: Dateien › Einstellungen › Standard-Apps – which program a double
	// click opens, per file type and account (preference "fileapps").
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { appsFor, catalogDefault, EDITOR, knownTypes, webAppKey } from './protocol.ts';
	import { fileApps, loadWebApps, setFileApp, titanosShows, webapps } from './webapps.svelte.ts';

	$effect(() => {
		if (!webapps.loaded) void loadWebApps();
	});
	const types = $derived(knownTypes(webapps.apps));
	const own = $derived(fileApps());
	let error = $state('');
	let busy = $state('');
	let saved = $state('');

	async function choose(ext: string, value: string) {
		error = '';
		saved = '';
		busy = ext;
		try {
			// The catalog's default needs no entry of its own.
			const fallback = catalogDefault(ext, webapps.apps, titanosShows('x.' + ext));
			const isDefault = value === (fallback ? webAppKey(fallback.id) : EDITOR);
			await setFileApp(ext, isDefault ? null : value);
			saved = ext;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
	}
	function current(ext: string): string {
		const mine = own[ext];
		if (mine === EDITOR || appsFor(ext, webapps.apps).some((a) => webAppKey(a.id) === mine))
			return mine;
		const fallback = catalogDefault(ext, webapps.apps, titanosShows('x.' + ext));
		return fallback ? webAppKey(fallback.id) : EDITOR;
	}
</script>

<!-- PLAN 78-1: a group of rows, one per file type; the choice is stored on
     change, as before. -->
<SettingGroup
	id="standard-apps"
	title={t('webapps.standard.title')}
	desc={t('webapps.standard.intro')}
>
	{#if !webapps.loaded}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else if !types.length}
		<p class="muted">{t('webapps.standard.none')}</p>
	{:else}
		{#each types as ext (ext)}
			{@const fit = appsFor(ext, webapps.apps)}
			<SettingRow icon="file" title=".{ext}" for="std-{ext}">
				{#snippet control()}
					{#if saved === ext}<span class="muted small" role="status"
							>{t('webapps.standard.saved')}</span
						>{/if}
					{#if own[ext]}
						<button
							type="button"
							class="btn"
							disabled={busy === ext}
							onclick={async () => {
								error = '';
								busy = ext;
								try {
									await setFileApp(ext, null);
									saved = ext;
								} catch (e) {
									error = apiErrorText(e);
								} finally {
									busy = '';
								}
							}}>{t('webapps.standard.reset')}</button
						>
					{/if}
					<select
						id="std-{ext}"
						class="field"
						value={current(ext)}
						disabled={busy === ext}
						onchange={(e) => choose(ext, e.currentTarget.value)}
					>
						{#each fit as a (a.id)}<option value={webAppKey(a.id)}>{a.name}</option>{/each}
						<option value={EDITOR}>{t('webapps.standard.titanos')}</option>
					</select>
				{/snippet}
			</SettingRow>
		{/each}
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</SettingGroup>
