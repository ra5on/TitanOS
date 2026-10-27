<script lang="ts">
	// The assistant after the first start: name of the NAS and time zone,
	// a look at the network, and what to do next. Every step can be skipped.
	import NetworkBridgeSetup from '#lib/components/NetworkBridgeSetup.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	const STEPS = 3;
	let step = $state(1);
	let hostname = $state(session.data?.hostname ?? 'titanos');
	// The browser knows where it is; the NAS starts out in UTC.
	let timezone = $state(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC');
	const zones = $derived([...new Set([timezone, 'UTC', ...Intl.supportedValuesOf('timeZone')])]);
	let busy = $state(false);
	let error = $state('');
	const address = $derived(system.info?.network?.ipv4.split('/')[0] ?? location.hostname);

	async function saveBasics(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api.put('/system/timezone', { timezone });
			if (hostname !== session.data?.hostname) {
				await api.put('/system/hostname', { hostname });
				await refreshSession();
			}
			step = 2;
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			// In development mode nothing is changed; the assistant still shows its steps.
			if (code === 'read_only') step = 2;
			else error = errorText(code);
		}
		busy = false;
	}
</script>

<div class="frame">
	<section class="tile">
		<div class="head">
			<div>
				<h1>{t('welcome.title')}</h1>
				<p class="muted">{t('welcome.step', { n: step, total: STEPS })}</p>
			</div>
			<LanguageSwitch />
		</div>

		{#if step === 1}
			<form onsubmit={saveBasics}>
				<h2>{t('welcome.basics')}</h2>
				<p class="muted">{t('welcome.intro')}</p>
				<label
					><span>{t('welcome.hostname')}</span>
					<input
						class="field mono"
						type="text"
						bind:value={hostname}
						maxlength="63"
						pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
						autocapitalize="none"
						spellcheck="false"
						required
					/>
					<small>{t('welcome.hostnameHint')}</small>
				</label>
				<label
					><span>{t('welcome.timezone')}</span>
					<select class="field" bind:value={timezone}
						>{#each zones as zone (zone)}<option value={zone}>{zone}</option>{/each}</select
					>
					<small>{t('welcome.timezoneHint')}</small>
				</label>
				{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 2)}>{t('welcome.skip')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}>{t('welcome.next')}</button>
				</div>
			</form>
		{:else if step === 2}
			<div class="body">
				<h2>{t('welcome.network')}</h2>
				<p>{t('welcome.networkNow', { address })}</p>
				<p class="muted">{t('welcome.networkHint')}</p>
				<NetworkBridgeSetup auto />
				<a href="/settings/network">{t('welcome.networkSettings')}</a>
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 1)}>{t('welcome.back')}</button>
					<button type="button" class="btn btn-primary" onclick={() => (step = 3)}
						>{t('welcome.next')}</button
					>
				</div>
			</div>
		{:else}
			<div class="body">
				<h2>{t('welcome.done')}</h2>
				<p class="muted">{t('welcome.doneText')}</p>
				<div class="todos">
					{#each [['/storage', 'welcome.todoVolume'], ['/shares', 'welcome.todoShare'], ['/account', 'welcome.todoTwoFactor'], ['/docker', 'welcome.todoApps']] as const as [href, key] (href)}
						<a class="todo" {href}
							><span class="name">{t(key)}</span><span class="muted">{t(`${key}Text`)}</span></a
						>
					{/each}
				</div>
				<div class="actions">
					<button type="button" class="btn" onclick={() => (step = 2)}>{t('welcome.back')}</button>
					<a class="btn btn-primary" href="/">{t('welcome.finish')}</a>
				</div>
			</div>
		{/if}
	</section>
</div>

<style>
	.frame {
		display: flex;
		justify-content: center;
	}
	section {
		width: 100%;
		max-width: 640px;
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 28px;
		font-size: 0.875rem;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
	}
	h1 {
		font-size: 1.375rem;
		font-weight: 600;
	}
	h2 {
		font-size: 1.0625rem;
		font-weight: 600;
	}
	form,
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
		margin-top: 6px;
	}
	.todos {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: 10px;
	}
	.todo {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.todo:hover {
		border-color: var(--accent);
	}
	.name {
		font-weight: 600;
	}
</style>
