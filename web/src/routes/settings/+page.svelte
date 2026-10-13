<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let terminals = $state<{ enabled: boolean } | null>(null);
	let error = $state('');

	$effect(() => {
		if (admin) api.get<{ enabled: boolean }>('/terminal').then((res) => (terminals = res), () => {});
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
			terminals = await api.put<{ enabled: boolean }>('/terminal', { enabled });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
</script>

<h1>{t('settings.title')}</h1>

<div class="areas">
	<a class="tile" href="/settings/appearance">
		<span class="name">{t('menu.appearance')}</span>
		<span class="muted">{t('settings.appearanceDesc')}</span>
	</a>
	{#if admin}
		<a class="tile" href="/settings/network">
			<span class="name">{t('netcfg.title')}</span>
			<span class="muted">{t('netcfg.settingsDesc')}</span>
		</a>
		<a class="tile" href="/settings/hardware">
			<span class="name">{t('hardware.title')}</span>
			<span class="muted">{t('hardware.settingsDesc')}</span>
		</a>
	{/if}
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
		font-size: 1.5rem;
		font-weight: 600;
	}
	.areas {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: var(--gap);
	}
	a {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: var(--tile-pad);
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
