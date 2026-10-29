<script lang="ts">
	// "Watch reachability": the push healthcheck of IPv64 the NAS pings.
	// Nothing is created at IPv64 before it is switched on; switching off
	// asks whether to pause, delete or only stop pinging. Shared by
	// Settings › Dynamic DNS and Settings › IPv64 › DynDNS.
	import { ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		heartbeatChecks,
		heartbeatChoiceValid,
		heartbeatOff,
		heartbeatOn,
		heartbeatRemote,
		heartbeatTest,
		type DdnsStatus,
		type Ipv64Healthcheck,
		type Ipv64Integration,
	} from './ddns.ts';

	let {
		status,
		apiKey = '',
		keyKnown,
		onstatus,
	}: {
		status: DdnsStatus;
		/** A key typed in but not saved yet; empty takes the stored one. */
		apiKey?: string;
		keyKnown: boolean;
		onstatus: (next: DdnsStatus) => void;
	} = $props();

	function failure(err: unknown): string {
		if (err instanceof ApiError && err.code.startsWith('ipv64_') && err.code !== 'ipv64_no_key') return `${errorText(err.code)} ${err.message}`;
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}
	const when = (value?: string) => (value ? formatDateTime(new Date(value)) : '–');

	const beat = $derived(status.settings.heartbeat);
	let beatError = $state('');
	let beatBusy = $state(false);
	let switchingOn = $state(false);
	let switchingOff = $state(false);
	let checks = $state<Ipv64Healthcheck[]>([]);
	let integrations = $state<Ipv64Integration[]>([]);
	let choice = $state({ check: 'new', name: '', period: 30, grace: 10, integrations: [0], autoPause: true });
	let remote = $state<Ipv64Healthcheck | null>(null);
	let testWait = $state(false);
	let testNotice = $state('');

	async function run(action: () => Promise<void>) {
		beatBusy = true;
		beatError = '';
		try {
			await action();
		} catch (err) {
			beatError = failure(err);
		} finally {
			beatBusy = false;
		}
	}
	const startOn = () =>
		run(async () => {
			const answer = await heartbeatChecks(apiKey);
			checks = answer.healthchecks;
			integrations = answer.integrations;
			choice = { ...choice, check: 'new', name: answer.suggestedName };
			switchingOn = true;
		});
	const switchOn = () =>
		run(async () => {
			onstatus(await heartbeatOn(apiKey, choice));
			switchingOn = false;
		});
	const switchOff = (action: '' | 'pause' | 'delete') =>
		run(async () => {
			onstatus(await heartbeatOff(apiKey, action));
			switchingOff = false;
			remote = null;
		});
	const loadRemote = () =>
		run(async () => {
			remote = await heartbeatRemote(apiKey);
		});
	async function sendTest() {
		testWait = true;
		testNotice = beatError = '';
		setTimeout(() => (testWait = false), 30_000);
		try {
			await heartbeatTest(apiKey);
			testNotice = t('ipv64.testSent');
		} catch (err) {
			beatError = failure(err);
		}
	}
	function toggleIntegration(id: number, on: boolean) {
		choice.integrations = on ? [...new Set([...choice.integrations, id])] : choice.integrations.filter((x) => x !== id);
	}
</script>

<div class="part divided">
	<h3 id="ipv64-heartbeat">{t('ipv64.heartbeat')}</h3>
	<p class="muted hint">{t('ipv64.heartbeatHint')}</p>
	<label class="check"
		><input
			type="checkbox"
			checked={beat.enabled}
			disabled={beatBusy || (!beat.enabled && !keyKnown)}
			onchange={(event) => {
				const field = event.currentTarget;
				field.checked = beat.enabled;
				if (beat.enabled) switchingOff = true;
				else startOn();
			}}
		/><span>{t('ipv64.heartbeatEnable')}</span></label
	>
	{#if beat.enabled}
		<dl>
			<div><dt>{t('ipv64.check')}</dt><dd>{beat.check || '–'}</dd></div>
			<div><dt>{t('ipv64.lastPing')}</dt><dd>{when(status.heartbeat.lastPing)}</dd></div>
			<div><dt>{t('ipv64.nextPing')}</dt><dd>{when(status.heartbeat.nextPing)}</dd></div>
			<div><dt>{t('ipv64.autoPause')}</dt><dd>{t(beat.autoPause ? 'ipv64.yes' : 'ipv64.no')}</dd></div>
			{#if remote}
				<div><dt>{t('ipv64.remoteStatus')}</dt><dd>{remote.status}</dd></div>
				<div><dt>{t('ipv64.lastSeen')}</dt><dd>{when(remote.lastSeenAt)}</dd></div>
			{/if}
		</dl>
		{#if status.heartbeat.paused}<p class="notice-warn">{t('ipv64.pausedForShutdown')}</p>{/if}
		{#if status.heartbeat.lastError}<p class="notice-error" role="alert">{status.heartbeat.lastError}</p>{/if}
		<div class="row-actions">
			<button type="button" class="btn" disabled={!keyKnown || beatBusy} onclick={loadRemote}>{t('ipv64.remoteLoad')}</button>
			<button type="button" class="btn" disabled={!keyKnown || testWait} onclick={sendTest}>{t('ipv64.test')}</button>
		</div>
		{#if testNotice}<p class="muted" role="status">{testNotice}</p>{/if}
	{/if}
	{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
</div>

{#if switchingOn}
	<Dialog title={t('ipv64.onTitle')} locked={beatBusy} onclose={() => (switchingOn = false)}>
		<p>{t('ipv64.onText')}</p>
		<label class="stack"
			><span>{t('ipv64.check')}</span><select class="field" bind:value={choice.check}>
				<option value="new">{t('ipv64.newCheck')}</option>
				{#each checks as check (check.id)}<option value={check.id}>{check.name} ({check.status})</option>{/each}
			</select></label
		>
		{#if choice.check === 'new'}
			<label class="stack"><span>{t('ipv64.checkName')}</span><input class="field" type="text" bind:value={choice.name} maxlength="100" /></label>
			<label class="stack"><span>{t('ipv64.period')}</span><input class="field short" type="number" min="20" max="1440" bind:value={choice.period} /></label>
			<label class="stack"><span>{t('ipv64.grace')}</span><input class="field short" type="number" min="1" max="1440" bind:value={choice.grace} /></label>
			<p class="muted hint">{t('ipv64.periodHint')}</p>
			<fieldset>
				<legend>{t('ipv64.integrations')}</legend>
				{#each integrations as integration (integration.id)}
					<label class="check"
						><input
							type="checkbox"
							checked={choice.integrations.includes(integration.id)}
							onchange={(event) => toggleIntegration(integration.id, event.currentTarget.checked)}
						/><span>{integration.name} <span class="muted">({integration.type})</span></span></label
					>
				{/each}
			</fieldset>
		{/if}
		<label class="check"><input type="checkbox" bind:checked={choice.autoPause} /><span>{t('ipv64.autoPauseLabel')}</span></label>
		<p class="muted hint">{t('ipv64.autoPauseHint')}</p>
		{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={beatBusy} onclick={() => (switchingOn = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={beatBusy || !heartbeatChoiceValid(choice)} onclick={switchOn}>{t('ipv64.onSubmit')}</button>
		</div>
	</Dialog>
{/if}

{#if switchingOff}
	<Dialog title={t('ipv64.offTitle')} locked={beatBusy} onclose={() => (switchingOff = false)}>
		<p>{t('ipv64.offText', { check: beat.check || '' })}</p>
		{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={beatBusy} onclick={() => (switchingOff = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={beatBusy || !keyKnown} onclick={() => switchOff('delete')}>{t('ipv64.offDelete')}</button>
			<button type="button" class="btn btn-warn" disabled={beatBusy || !keyKnown} onclick={() => switchOff('pause')}>{t('ipv64.offPause')}</button>
			<button type="button" class="btn" disabled={beatBusy} onclick={() => switchOff('')}>{t('ipv64.offOnly')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.part {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	h3 {
		margin: 0;
		font-size: 0.9375rem;
	}
	.hint {
		margin: 0;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.short {
		max-width: 10rem;
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
</style>
