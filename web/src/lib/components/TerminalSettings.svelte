<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { page } from '$app/state';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	type Settings = { enabled: boolean; confirm: boolean; idleMinutes?: number };
	const IDLE = [5, 15, 30, 60, 120, 0];
	let idleState = $state<'' | 'saving' | 'saved'>('');
	let idleError = $state('');
	async function saveIdle(minutes: number) {
		idleState = 'saving';
		idleError = '';
		try {
			value = await api.put<Settings>('/terminal', { idleMinutes: minutes });
			idleState = 'saved';
			setTimeout(() => (idleState = ''), 5000);
		} catch (err) {
			idleState = '';
			idleError = apiErrorText(err);
		}
	}
	let value = $state<Settings | null>(null);
	let loading = $state(false);
	let loadError = $state('');
	let pending = $state<'enabled' | 'confirm' | null>(null);
	let saved = $state<'enabled' | 'confirm' | null>(null);
	let errors = $state({ enabled: '', confirm: '' });
	async function load() {
		loading = true;
		loadError = '';
		try {
			value = await api.get<Settings>('/terminal');
		} catch (err) {
			loadError = apiErrorText(err);
		} finally {
			loading = false;
		}
	}
	onMount(load);
	// A link to this group (#access-terminal): its rows arrive after the
	// browser's own anchor scroll, so bring it into view then.
	const loaded = $derived(!!value || !!loadError);
	$effect(() => {
		if (!loaded || page.url.hash !== '#access-terminal') return;
		requestAnimationFrame(() =>
			document.getElementById('access-terminal')?.scrollIntoView({ block: 'start' }),
		);
	});
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = null), 5000);
			return () => clearTimeout(timer);
		}
	});
	async function save(key: 'enabled' | 'confirm', checked: boolean) {
		if (pending) return;
		pending = key;
		saved = null;
		errors[key] = '';
		try {
			value = await api.put<Settings>('/terminal', { [key]: checked });
			saved = key;
		} catch (err) {
			errors[key] = apiErrorText(err);
		} finally {
			pending = null;
		}
	}
</script>

<!-- PLAN 78-1: every setting is a row of its own; each stores on change as before. -->
<SettingGroup title={t('settings.consoles')} id="access-terminal">
	{#if loading}<p class="loading" role="status">{t('common.loading')}</p>{/if}
	{#if loadError}<div class="notice-error" role="alert">
			{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button>
		</div>{/if}
	{#if value}
		{#each ['enabled', 'confirm'] as const as key (key)}
			<SettingRow
				icon={key === 'enabled' ? 'terminal' : 'lock'}
				title={t(key === 'enabled' ? 'settings.terminal' : 'settings.terminalConfirm')}
				desc={t(key === 'enabled' ? 'settings.terminalDesc' : 'settings.terminalConfirmDesc')}
				for="terminal-{key}"
			>
				{#snippet control()}
					{#if pending === key || saved === key}<span role="status"
							>{t(pending === key ? 'ui.saving' : 'ui.saved')}</span
						>{/if}
					<input
						id="terminal-{key}"
						class="switch"
						type="checkbox"
						role="switch"
						disabled={pending !== null}
						checked={value?.[key]}
						onchange={async (event) => {
							const field = event.currentTarget;
							await save(key, field.checked);
							field.checked = value?.[key] ?? false;
						}}
					/>
				{/snippet}
			</SettingRow>
			{#if errors[key]}<p class="notice-error" role="alert">{errors[key]}</p>{/if}
		{/each}
		<!-- PLAN 62-3: how long a console or VM screen may go without input. -->
		<SettingRow
			icon="clock"
			title={t('settings.consoleIdle')}
			desc={t('settings.consoleIdleDesc')}
			for="terminal-idle"
		>
			{#snippet control()}
				{#if idleState}<span role="status"
						>{t(idleState === 'saving' ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
				<select
					id="terminal-idle"
					class="field"
					disabled={idleState === 'saving'}
					value={value?.idleMinutes ?? 15}
					onchange={(e) => saveIdle(Number(e.currentTarget.value))}
				>
					{#each IDLE as minutes (minutes)}
						<option value={minutes}
							>{minutes
								? t('settings.consoleIdleMinutes', { minutes })
								: t('settings.consoleIdleNever')}</option
						>
					{/each}
				</select>
			{/snippet}
		</SettingRow>
		{#if idleError}<p class="notice-error" role="alert">{idleError}</p>{/if}
	{/if}
</SettingGroup>
