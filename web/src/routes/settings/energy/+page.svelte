<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	type Settings = { governor: string; preference: string; platform: string };
	type Control = { key: keyof Settings; choices: string[]; current: string; mixed: boolean };
	type Status = { settings: Settings; controls: Control[]; drivers: string[]; readOnly: boolean; error?: string };
	let status = $state<Status | null>(null);
	let draft = $state<Settings>({ governor: '', preference: '', platform: '' });
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	const label = (value: string) => tOr(`energy.profile.${value}`, value);
	function take(result: Status) { status = result; draft = { ...result.settings }; }
	function fail(err: unknown) { error = errorText(err instanceof ApiError ? err.code : 'internal') + (err instanceof ApiError ? `\n${err.message}` : ''); }
	$effect(() => { api.get<Status>('/system/energy').then(take, fail); });
	async function save(event: SubmitEvent) {
		event.preventDefault(); busy = true; error = ''; saved = false;
		try { take(await api.put<Status>('/system/energy', draft)); saved = true; }
		catch (err) { fail(err); }
		finally { busy = false; }
	}
</script>

<h1>{t('energy.title')}</h1>
<p class="muted">{t('energy.intro')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status}
	{#if status.error}<p class="notice-warn" role="status">{t('energy.startupError')} {status.error}</p>{/if}
	{#if status.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}
	{#if status.controls.length === 0}
		<section class="tile"><p>{t('energy.unsupported')}</p></section>
	{:else}
		<form onsubmit={save}>
			{#each status.controls as control (control.key)}
				<section class="tile">
					<label for="energy-{control.key}">{t(`energy.${control.key}`)}</label>
					<p class="muted">{t(`energy.${control.key}Hint`)}</p>
					<select id="energy-{control.key}" class="field" bind:value={draft[control.key]} disabled={busy || status.readOnly}>
						<option value="">{t('energy.unmanaged')}</option>
						{#if draft[control.key] && !control.choices.includes(draft[control.key])}<option value={draft[control.key]} disabled>{draft[control.key]} — {t('energy.noLongerAvailable')}</option>{/if}
						{#each control.choices as choice (choice)}<option value={choice}>{label(choice)}{label(choice) !== choice ? ` (${choice})` : ''}</option>{/each}
					</select>
					<p class="muted current">{t('energy.current')}: {control.mixed ? t('energy.mixed') : label(control.current)}</p>
				</section>
			{/each}
			{#if status.drivers.length}<p class="muted">{t('energy.driver')}: <span class="mono">{status.drivers.join(', ')}</span></p>{/if}
			<button class="btn btn-primary" disabled={busy || status.readOnly}>{t('common.save')}</button>
			{#if saved}<p role="status">{t('energy.saved')}</p>{/if}
		</form>
	{/if}
{:else if !error}<p>{t('common.loading')}</p>{/if}

<style>
	h1 { font-size: 1.5rem; font-weight: 600; }
	form { display: flex; flex-direction: column; gap: 16px; align-items: flex-start; }
	.tile { width: 100%; max-width: 640px; padding: var(--tile-pad); display: flex; flex-direction: column; gap: 10px; }
	label { font-weight: 600; }
	p { margin: 0; }
	.current { font-size: 0.8125rem; }
	.notice-error { white-space: pre-wrap; }
</style>
