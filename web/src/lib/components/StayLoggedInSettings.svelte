<script lang="ts">
	// Settings › Access › Stay logged in: how long every login lasts, also
	// over a restart of the NAS. Applies to the logins there are as well.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Login = { stayDays: number; choices: number[] };
	let login = $state<Login | null>(null);
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	$effect(() => { if (saved) { const timer = setTimeout(() => saved = false, 5000); return () => clearTimeout(timer); } });

	onMount(async () => {
		try {
			login = await api.get<Login>('/system/login');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	});

	function label(days: number): string {
		if (days === 0) return t('stay.off');
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
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" id="access-stay" aria-labelledby="access-stay-title">
	<h2 class="tile-title" id="access-stay-title">{t('stay.title')}</h2>
	<p class="muted">{t('stay.intro')}</p>
	{#if login}
		<label class="stack"><span>{t('stay.duration')}</span>
			<select class="field" disabled={busy} value={login.stayDays} onchange={async (event) => { const field = event.currentTarget; await save(Number(field.value)); field.value = String(login?.stayDays ?? 0); }}>
				{#each login.choices as days}<option value={days}>{label(days)}</option>{/each}
			</select>
		</label>
		{#if login.stayDays !== 0}<p class="muted small">{t('stay.hint')}</p>{/if}
		{#if login.stayDays < 0}<p class="notice-warn">{t('stay.foreverWarn')}</p>{/if}
		{#if saved}<p class="muted small" role="status">{t('appearance.saved')}</p>{/if}
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>
