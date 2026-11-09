<script lang="ts">
	// PLAN 66: Dateien › Einstellungen › Standard-Apps – which program a double
	// click opens, per file type and account (preference "fileapps").
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

<section class="tile panel" aria-labelledby="standard-apps">
	<h2 class="tile-title" id="standard-apps">{t('webapps.standard.title')}</h2>
	<p class="muted">{t('webapps.standard.intro')}</p>
	{#if !webapps.loaded}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else if !types.length}
		<p class="muted">{t('webapps.standard.none')}</p>
	{:else}
		<div class="rows">
			{#each types as ext (ext)}
				{@const fit = appsFor(ext, webapps.apps)}
				<div class="row">
					<label class="ext" for="std-{ext}"><span class="mono">.{ext}</span></label>
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
					{#if saved === ext}<span class="muted small" role="status"
							>{t('webapps.standard.saved')}</span
						>{/if}
				</div>
			{/each}
		</div>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>

<style>
	.rows {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.ext {
		min-width: 64px;
	}
	.row select {
		flex: 1 1 180px;
		max-width: 320px;
	}
</style>
