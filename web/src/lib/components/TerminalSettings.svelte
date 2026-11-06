<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
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

<section class="tile panel" id="access-terminal" use:revealTarget aria-labelledby="terminal-title">
	<h2 class="tile-title" id="terminal-title">{t('settings.consoles')}</h2>
	{#if loading}<p class="loading" role="status">{t('common.loading')}</p>{/if}
	{#if loadError}<div class="notice-error" role="alert">
			{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button>
		</div>{/if}
	{#if value}
		{#each ['enabled', 'confirm'] as const as key}
			<div class="option">
				<label class="check"
					><input
						type="checkbox"
						disabled={pending !== null}
						checked={value[key]}
						onchange={async (event) => {
							const field = event.currentTarget;
							await save(key, field.checked);
							field.checked = value?.[key] ?? false;
						}}
					/><span>{t(key === 'enabled' ? 'settings.terminal' : 'settings.terminalConfirm')}</span
					></label
				>
				<p class="muted">
					{t(key === 'enabled' ? 'settings.terminalDesc' : 'settings.terminalConfirmDesc')}
				</p>
				{#if pending === key || saved === key}<p role="status">
						{t(pending === key ? 'ui.saving' : 'ui.saved')}
					</p>{/if}
				{#if errors[key]}<p class="notice-error" role="alert">{errors[key]}</p>{/if}
			</div>
		{/each}
		<!-- PLAN 62-3: how long a console or VM screen may go without input. -->
		<div class="option">
			<label class="idle" for="terminal-idle">{t('settings.consoleIdle')}</label>
			<select
				id="terminal-idle"
				class="field"
				disabled={idleState === 'saving'}
				value={value.idleMinutes ?? 15}
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
			<p class="muted plain">{t('settings.consoleIdleDesc')}</p>
			{#if idleState}<p role="status">
					{t(idleState === 'saving' ? 'ui.saving' : 'ui.saved')}
				</p>{/if}
			{#if idleError}<p class="notice-error" role="alert">{idleError}</p>{/if}
		</div>
	{/if}
</section>

<style>
	.option {
		display: grid;
		gap: var(--space-1);
	}
	.option > p {
		padding-left: 28px;
		font-size: var(--text-base);
	}
	.option > p.plain {
		padding-left: 0;
	}
	.idle {
		font-weight: 600;
		margin-top: var(--space-3);
	}
	select {
		max-width: 260px;
	}
</style>
