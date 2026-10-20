<script lang="ts">
	import { matchesSetting } from '#lib/settings-search.ts';
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const areas: { href: string; label: MessageKey; admin?: boolean }[] = [
		{ href: 'appearance', label: 'menu.appearance' },
		{ href: 'network', label: 'netcfg.title', admin: true },
		{ href: 'notifications', label: 'notify.settingsTitle', admin: true },
		{ href: 'updates', label: 'update.title', admin: true },
		{ href: 'access', label: 'access.title', admin: true },
		{ href: 'backups', label: 'backup.title', admin: true },
		{ href: 'stick', label: 'stick.title', admin: true },
		{ href: 'ups', label: 'ups.title', admin: true },
		{ href: 'hardware', label: 'hardware.title', admin: true },
		{ href: 'power-button', label: 'powerButton.title', admin: true },
		{ href: 'memory', label: 'memoryPolicy.title', admin: true },
		{ href: 'energy', label: 'energy.title', admin: true },
		{ href: 'temperatures', label: 'temperatures.title' },
		{ href: 'about', label: 'about.title' }
	];
	const sortedAreas = $derived(areas.filter((area) => !area.admin || admin).toSorted((a, b) => t(a.label).localeCompare(t(b.label), i18n.locale)));

	let search = $state('');
	let saving = $state(false);
	let saved = $state(false);
	const visibleAreas = $derived(sortedAreas.filter(area => matchesSetting(search, t(area.label), area.href)));
	const showZone = $derived(matchesSetting(search, t('settings.timezone'), 'timezone'));
	const showTerminal = $derived(matchesSetting(search, t('settings.terminal'), 'terminal'));
	const resultCount = $derived(visibleAreas.length + (admin && showZone ? 1 : 0) + (admin && showTerminal ? 1 : 0));
	let terminals = $state<{ enabled: boolean; confirm: boolean } | null>(null);
	let error = $state('');

	$effect(() => {
		if (admin) api.get<{ enabled: boolean; confirm: boolean }>('/terminal').then((res) => (terminals = res), () => {});
	});

	let timezone = $state('');
	// The browser knows the names of the time zones; the NAS checks the choice.
	const zones = $derived([...new Set([timezone, 'UTC', ...Intl.supportedValuesOf('timeZone')])].filter(Boolean));
	$effect(() => {
		if (admin) api.get<{ timezone: string }>('/network').then((res) => (timezone = res.timezone), () => {});
	});
	async function setTimezone(zone: string) {
		if (saving) return;
		saving = true; saved = false;
		error = '';
		try {
			await api.put('/system/timezone', { timezone: zone });
			timezone = zone;
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally { saving = false; }
	}

	async function setTerminals(enabled: boolean) {
		if (saving) return;
		saving = true; saved = false;
		error = '';
		try {
			terminals = await api.put<{ enabled: boolean; confirm: boolean }>('/terminal', { enabled });
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally { saving = false; }
	}

	async function setConfirm(confirm: boolean) {
		if (saving) return;
		saving = true; saved = false;
		error = '';
		try {
			terminals = await api.put<{ enabled: boolean; confirm: boolean }>('/terminal', { confirm });
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally { saving = false; }
	}
</script>

<PageHead title={t('settings.title')} />

<label class="search"><span>{t('ui.searchSettings')}</span><input class="field" type="search" bind:value={search} placeholder={t('ui.searchHint')} /></label>
{#if search}<div class="search-result"><span role="status">{t('ui.results', { count: resultCount })}</span><button class="btn" type="button" onclick={() => search = ''}>{t('ui.clear')}</button></div>{/if}
{#if search && resultCount === 0}<p>{t('ui.noSettings')}</p>{/if}
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if saving || saved}<p class="notice-ok" role="status">{t(saving ? 'ui.saving' : 'ui.saved')}</p>{/if}
<div class="areas">
	{#each visibleAreas as area (area.href)}
		<a class="tile" href="/settings/{area.href}"><span class="name">{t(area.label)}</span></a>
	{/each}
</div>

{#if admin && timezone && showZone}
	<section class="tile option">
		<label class="zone"><span class="name">{t('settings.timezone')}</span>
			<select class="field" disabled={saving} value={timezone} onchange={async (event) => { const field = event.currentTarget; await setTimezone(field.value); field.value = timezone; }}>
				{#each zones as zone (zone)}<option value={zone}>{zone}</option>{/each}
			</select>
		</label>
		<p class="muted">{t('settings.timezoneDesc')}</p>
	</section>
{/if}

{#if admin && terminals && showTerminal}
	<section class="tile option">
		<label class="check">
			<input type="checkbox" disabled={saving} checked={terminals.enabled} onchange={async (event) => { const field = event.currentTarget; await setTerminals(field.checked); field.checked = terminals?.enabled ?? false; }} />
			<span class="name">{t('settings.terminal')}</span>
		</label>
		<p class="muted">{t('settings.terminalDesc')}</p>
		<label class="check">
			<input type="checkbox" disabled={saving} checked={terminals.confirm} onchange={async (event) => { const field = event.currentTarget; await setConfirm(field.checked); field.checked = terminals?.confirm ?? false; }} />
			<span class="name">{t('settings.terminalConfirm')}</span>
		</label>
		<p class="muted">{t('settings.terminalConfirmDesc')}</p>

	</section>
{/if}

<style>
	.search { display: grid; gap: 8px; max-width: 40rem; }
	.search-result { display: flex; align-items: center; gap: 12px; }
	.option {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: var(--tile-pad);
		font-size: 0.875rem;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.zone {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
	}
	.zone select {
		width: auto;
		min-width: min(16rem, 100%);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.areas {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 180px), 1fr));
		gap: 10px;
	}
	a {
		display: flex;
		flex-direction: column;
		justify-content: center;
		min-height: 48px;
		padding: 10px 14px;
		color: var(--text);
		text-decoration: none;
		font-size: 0.875rem;
	}
	a:hover {
		border-color: var(--accent);
	}
	.name {
		font-size: 1rem;
		font-weight: 600;
	}
</style>
