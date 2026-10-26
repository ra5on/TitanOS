<script lang="ts">
	// Confirmation before a restart or shutdown, and the waiting screen after it.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';

	type Props = { action: 'restart' | 'shutdown'; onclose: () => void };
	let { action, onclose }: Props = $props();

	let phase = $state<'confirm' | 'working' | 'done'>('confirm');
	let error = $state('');

	async function confirm() {
		error = '';
		phase = 'working';
		try {
			await api.post('/system/power', { action: action === 'restart' ? 'reboot' : 'poweroff' });
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			phase = 'confirm';
			return;
		}
		phase = 'done';
		if (action === 'restart') waitForRestart();
	}

	// The NAS first has to go down, then come back: wait for an answer that
	// follows at least one failed request (or half a minute of silence).
	async function waitForRestart() {
		const started = Date.now();
		let wasDown = false;
		for (;;) {
			await new Promise((resolve) => setTimeout(resolve, 3000));
			try {
				const res = await fetch('/api/v1/health', { cache: 'no-store' });
				if (res.ok && (wasDown || Date.now() - started > 30000)) {
					location.reload();
					return;
				}
			} catch {
				wasDown = true;
			}
		}
	}
</script>

<!-- Once the NAS is going down there is nothing to return to. -->
<Dialog
	title={t(
		phase !== 'done'
			? action === 'restart' ? 'power.restartTitle' : 'power.shutdownTitle'
			: action === 'restart' ? 'power.restarting' : 'power.shuttingDown'
	)}
	locked={phase !== 'confirm'}
	{onclose}
>
	{#if phase !== 'done'}
		<ul>
			<li><span class="dot" aria-hidden="true"></span>{t('power.servicesNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.workloadsNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.maintenanceNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.diskTestNote')}</li>
			{#if action === 'shutdown'}<li class="muted"><span class="dot" aria-hidden="true"></span>{t('power.wakeOnLanNote')}</li>{/if}
			<li class="muted"><span class="dot" aria-hidden="true"></span>{t(action === 'restart' ? 'power.restartNote' : 'power.shutdownNote')}</li>
		</ul>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={phase === 'working'}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={confirm} disabled={phase === 'working'}>
				{t(action === 'restart' ? 'power.restartAction' : 'power.shutdownAction')}
			</button>
		</div>
	{:else}
		<p class="muted" role="status">{t(action === 'restart' ? 'power.restartingText' : 'power.shutDownText')}</p>
	{/if}
</Dialog>

<style>
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 10px;
		font-size: 0.875rem;
	}
	li {
		display: flex;
		gap: 10px;
	}
	li .dot {
		margin-top: 7px;
	}
</style>
