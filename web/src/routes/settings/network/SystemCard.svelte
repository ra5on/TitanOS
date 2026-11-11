<script lang="ts">
	// Name of the NAS and its time zone. A new name cannot cut the
	// connection, so it applies at once and is not part of the draft.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	let {
		hostname,
		timezone,
		disabled,
		onsaved,
	}: { hostname: string; timezone: string; disabled: boolean; onsaved: (name: string) => void } =
		$props();

	let value = $state(''),
		edited = $state(false),
		busy = $state(false),
		error = $state(''),
		saved = $state(false);
	$effect(() => {
		if (!edited) value = hostname;
	});
	const valid = $derived(/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(value));
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api.put('/system/hostname', { hostname: value });
			edited = false;
			saved = true;
			clearTimeout(timer);
			timer = setTimeout(() => (saved = false), 5000);
			onsaved(value);
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: the name is a row with its field and its own button (typed
     text is never stored half-way); the time zone leads to its page. -->
<SettingGroup title={t('netx.system')} id="net-system">
	<form onsubmit={save}>
		<SettingRow
			icon="home"
			title={t('netcfg.hostname')}
			desc={t('netcfg.hostnameHint')}
			for="net-hostname"
		>
			{#snippet control()}
				<input
					id="net-hostname"
					class="field mono"
					type="text"
					maxlength="63"
					autocapitalize="none"
					spellcheck="false"
					required
					bind:value
					oninput={() => {
						edited = true;
						saved = false;
					}}
					{disabled}
				/>
				<button
					class="btn btn-primary"
					disabled={disabled || busy || !edited || !valid || value === hostname}
					>{t('netx.saveHostname')}</button
				>
				{#if saved}<span class="chip ok" role="status">{t('netx.hostnameSaved')}</span>{/if}
			{/snippet}
		</SettingRow>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<SettingRow icon="clock" title={t('netx.timezone')} href="/settings/timezone">
		{#snippet control()}<span class="mono">{timezone || '–'}</span>{/snippet}
	</SettingRow>
</SettingGroup>

<style>
	form {
		display: contents;
	}
</style>
