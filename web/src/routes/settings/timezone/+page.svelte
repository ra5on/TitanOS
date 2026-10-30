<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let timezone = $state<string | null>(null);
	let loading = $state(false);
	let saving = $state(false);
	let saved = $state(false);
	let loadError = $state('');
	let error = $state('');
	const zones = $derived(
		[...new Set([timezone, 'UTC', ...Intl.supportedValuesOf('timeZone')])].filter(
			(v): v is string => !!v,
		),
	);
	async function load() {
		loading = true;
		loadError = '';
		try {
			timezone = (await api.get<{ timezone: string }>('/network')).timezone;
		} catch (err) {
			loadError = apiErrorText(err);
		} finally {
			loading = false;
		}
	}
	onMount(load);
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});
	async function save(zone: string) {
		if (saving) return;
		saving = true;
		saved = false;
		error = '';
		try {
			await api.put('/system/timezone', { timezone: zone });
			timezone = zone;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			saving = false;
		}
	}
</script>

<PageHead title={t('settings.datetime')} parent={{ href: '/settings', label: t('nav.settings') }} />
<section class="tile panel">
	{#if loading}<p class="loading" role="status">{t('common.loading')}</p>{/if}
	{#if loadError}<div class="notice-error" role="alert">
			{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button>
		</div>{/if}
	{#if timezone !== null}
		<label class="stack"
			><span>{t('settings.timezone')}</span><select
				class="field"
				disabled={saving}
				value={timezone}
				onchange={async (event) => {
					const field = event.currentTarget;
					await save(field.value);
					field.value = timezone ?? '';
				}}
				>{#each zones as zone}<option value={zone}>{zone}</option>{/each}</select
			></label
		>
		{#if saving || saved}<p role="status">{t(saving ? 'ui.saving' : 'ui.saved')}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{/if}
	<p class="muted">{t('settings.timeHint')}</p>
</section>
