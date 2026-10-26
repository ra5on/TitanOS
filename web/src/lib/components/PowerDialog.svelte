<script lang="ts">
	// Confirmation before a restart or shutdown, and the waiting screen after it.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';

	type Props = { action: 'restart' | 'shutdown'; onclose: () => void };
	let { action, onclose }: Props = $props();

	type PowerStatus = { pending: { action: string; by: string; since: string } | null; rebuilding: boolean };
	let phase = $state<'confirm' | 'working' | 'done' | 'waiting'>('confirm');
	let error = $state('');
	let status = $state<PowerStatus | null>(null);
	// Ticked by default while a rebuild runs: a restart would start it over.
	let afterRebuild = $state(true);

	$effect(() => {
		api.get<PowerStatus>('/system/power')
			.then((res) => (status = res))
			.catch(() => (status = null));
	});

	async function confirm() {
		error = '';
		phase = 'working';
		const wait = !!status?.rebuilding && afterRebuild;
		try {
			const res = await api.post<{ pending?: boolean } | null>('/system/power', {
				action: action === 'restart' ? 'reboot' : 'poweroff',
				afterRebuild: wait
			});
			if (wait && res?.pending) {
				phase = 'waiting';
				return;
			}
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			phase = 'confirm';
			return;
		}
		phase = 'done';
		if (action === 'restart') waitForRestart();
	}

	async function cancelWaiting() {
		error = '';
		try {
			status = await api.delete<PowerStatus>('/system/power');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
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
	locked={phase === 'working' || phase === 'done'}
	{onclose}
>
	{#if phase === 'waiting'}
		<p role="status">{t(action === 'restart' ? 'power.waitingRestart' : 'power.waitingShutdown')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		</div>
	{:else if phase !== 'done'}
		{#if status?.pending}
			<div class="notice-warn" role="status">
				{t(status.pending.action === 'reboot' ? 'power.pendingRestart' : 'power.pendingShutdown', { user: status.pending.by })}
				<button type="button" class="btn small" onclick={cancelWaiting}>{t('power.cancelWaiting')}</button>
			</div>
		{/if}
		<ul>
			<li><span class="dot" aria-hidden="true"></span>{t('power.servicesNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.workloadsNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.maintenanceNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.diskTestNote')}</li>
			{#if action === 'shutdown'}<li class="muted"><span class="dot" aria-hidden="true"></span>{t('power.wakeOnLanNote')}</li>{/if}
			<li class="muted"><span class="dot" aria-hidden="true"></span>{t(action === 'restart' ? 'power.restartNote' : 'power.shutdownNote')}</li>
		</ul>
		{#if status?.rebuilding}
			<label class="check"><input type="checkbox" bind:checked={afterRebuild} />{t('power.afterRebuild')}</label>
		{/if}
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
