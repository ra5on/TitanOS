<script lang="ts">
	// Settings › Access › Stay logged in: how long every login lasts, also
	// over a restart of the NAS. Applies to the logins there are as well.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { page } from '$app/state';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Login = { stayDays: number; choices: number[] };
	let login = $state<Login | null>(null);
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});

	onMount(async () => {
		try {
			login = await api.get<Login>('/system/login');
		} catch (err) {
			error = apiErrorText(err);
		}
	});

	// A link to this row (#access-stay): the rows above it arrive after the
	// browser's own anchor scroll, so bring it into view once it has its state.
	$effect(() => {
		if (!login || page.url.hash !== '#access-stay') return;
		requestAnimationFrame(() =>
			document.getElementById('access-stay')?.scrollIntoView({ block: 'start' }),
		);
	});

	// Short choices (a long one is clipped in the phone's select); what "Aus"
	// means stands in the row's explanation.
	function label(days: number): string {
		if (days === 0) return t('stay.offShort');
		if (days < 0) return t('stay.forever');
		return days === 365 ? t('stay.year') : t('stay.days', { count: days });
	}

	async function save(days: number) {
		busy = true;
		error = '';
		try {
			login = await api.put<Login>('/system/login', { stayDays: days });
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: one row of the group "Weboberfläche"; the choice stores on change as before. -->
<SettingRow id="access-stay" icon="clock" title={t('stay.title')} for="access-stay-select">
	{#snippet description()}
		{t('stay.intro')}
		{#if login?.stayDays === 0}{t('stay.offHint')}{/if}
	{/snippet}
	{#snippet control()}
		{#if saved}<span role="status">{t('appearance.saved')}</span>{/if}
		{#if login}
			<select
				id="access-stay-select"
				class="field"
				aria-label={t('stay.duration')}
				disabled={busy}
				value={login.stayDays}
				onchange={async (event) => {
					const field = event.currentTarget;
					await save(Number(field.value));
					field.value = String(login?.stayDays ?? 0);
				}}
			>
				{#each login.choices as days (days)}<option value={days}>{label(days)}</option>{/each}
			</select>
		{/if}
	{/snippet}
</SettingRow>
{#if login && login.stayDays !== 0}<p class="muted">{t('stay.hint')}</p>{/if}
{#if login && login.stayDays < 0}<p class="notice-warn">{t('stay.foreverWarn')}</p>{/if}
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
