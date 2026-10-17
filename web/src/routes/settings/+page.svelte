<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
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
		error = '';
		try {
			await api.put('/system/timezone', { timezone: zone });
			timezone = zone;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	async function setTerminals(enabled: boolean) {
		error = '';
		try {
			terminals = await api.put<{ enabled: boolean; confirm: boolean }>('/terminal', { enabled });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	async function setConfirm(confirm: boolean) {
		error = '';
		try {
			terminals = await api.put<{ enabled: boolean; confirm: boolean }>('/terminal', { confirm });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
</script>

<h1>{t('settings.title')}</h1>

<div class="areas">
	{#each sortedAreas as area (area.href)}
		<a class="tile" href="/settings/{area.href}"><span class="name">{t(area.label)}</span></a>
	{/each}
</div>

{#if admin && timezone}
	<section class="tile option">
		<label class="zone"><span class="name">{t('settings.timezone')}</span>
			<select class="field" value={timezone} onchange={(event) => setTimezone(event.currentTarget.value)}>
				{#each zones as zone (zone)}<option value={zone}>{zone}</option>{/each}
			</select>
		</label>
		<p class="muted">{t('settings.timezoneDesc')}</p>
	</section>
{/if}

{#if admin && terminals}
	<section class="tile option">
		<label class="check">
			<input type="checkbox" checked={terminals.enabled} onchange={(event) => setTerminals(event.currentTarget.checked)} />
			<span class="name">{t('settings.terminal')}</span>
		</label>
		<p class="muted">{t('settings.terminalDesc')}</p>
		<label class="check">
			<input type="checkbox" checked={terminals.confirm} onchange={(event) => setConfirm(event.currentTarget.checked)} />
			<span class="name">{t('settings.terminalConfirm')}</span>
		</label>
		<p class="muted">{t('settings.terminalConfirmDesc')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>
{/if}

<style>
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
		min-width: 16rem;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	h1 {
		font-size: var(--text-page);
		font-weight: 600;
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
