<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	// Confirmation before a restart or shutdown, and the waiting screen after it.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';

	type Props = { action: 'restart' | 'shutdown'; onclose: () => void };
	let { action, onclose }: Props = $props();

	type Item = {
		kind: 'vm' | 'lxc' | 'docker';
		name: string;
		state: 'stopping' | 'stopped' | 'failed';
		error?: string;
	};
	type Preparing = {
		action: 'reboot' | 'poweroff';
		by: string;
		phase: 'stopping' | 'waiting' | 'going' | 'failed';
		items: Item[];
		error?: string;
	};
	type PowerStatus = {
		pending: { action: string; by: string; since: string } | null;
		rebuilding: boolean;
		preparing?: Preparing | null;
	};
	let phase = $state<'confirm' | 'working' | 'preparing' | 'done' | 'waiting'>('confirm');
	let error = $state('');
	let status = $state<PowerStatus | null>(null);
	let busy = $state(false);
	// Ticked by default while a rebuild runs: a restart would start it over.
	let afterRebuild = $state(true);
	const preparing = $derived(status?.preparing ?? null);
	// A preparation under way decides which way the NAS goes, not the button.
	const restart = $derived(preparing ? preparing.action === 'reboot' : action === 'restart');
	const stuck = $derived(preparing?.items.filter((item) => item.state !== 'stopped') ?? []);

	async function load() {
		try {
			status = await api.get<PowerStatus>('/system/power');
		} catch {
			// While the NAS goes down the answers stop: it is on its way.
			if (phase === 'preparing') finish();
			return;
		}
		if (status?.preparing && phase === 'confirm') phase = 'preparing';
		if (phase !== 'preparing') return;
		if (status?.preparing?.phase === 'going' || (!status?.preparing && !busy)) finish();
	}
	$effect(() => {
		void load();
	});
	// While preparing, the list follows the guests.
	$effect(() => {
		if (phase !== 'preparing') return;
		// Not paused in a hidden tab: the end of the shutdown must be noticed.
		return poll(load, { interval: 2000, immediate: false, pauseHidden: false });
	});

	function finish() {
		phase = 'done';
		if (restart) waitForRestart();
	}

	async function confirm() {
		error = '';
		phase = 'working';
		const wait = !!status?.rebuilding && afterRebuild;
		try {
			const res = await api.post<{ pending?: boolean } | null>('/system/power', {
				action: action === 'restart' ? 'reboot' : 'poweroff',
				afterRebuild: wait,
				wait: true,
			});
			if (wait && res?.pending) {
				phase = 'waiting';
				return;
			}
		} catch (err) {
			error = apiErrorText(err);
			phase = 'confirm';
			return;
		}
		busy = true;
		phase = 'preparing';
		// The first list comes a moment after the request.
		setTimeout(() => {
			busy = false;
			void load();
		}, 600);
	}

	async function anyway() {
		error = '';
		try {
			await api.post('/system/power/now', {});
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	async function cancelWaiting() {
		error = '';
		try {
			status = await api.delete<PowerStatus>('/system/power');
			if (phase === 'preparing') onclose();
		} catch (err) {
			error = apiErrorText(err);
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
				const res = await fetch(apiPath('/health'), { cache: 'no-store' });
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
		phase === 'preparing'
			? restart
				? 'power.preparingRestart'
				: 'power.preparingShutdown'
			: phase !== 'done'
				? restart
					? 'power.restartTitle'
					: 'power.shutdownTitle'
				: restart
					? 'power.restarting'
					: 'power.shuttingDown',
	)}
	locked={phase === 'working' || phase === 'done' || phase === 'preparing'}
	{onclose}
>
	{#if phase === 'waiting'}
		<p role="status">
			{t(action === 'restart' ? 'power.waitingRestart' : 'power.waitingShutdown')}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		</div>
	{:else if phase === 'preparing'}
		<p class="muted" role="status">
			{t(restart ? 'power.preparingTextRestart' : 'power.preparingTextShutdown')}
		</p>
		{#if preparing && preparing.items.length}
			<ul class="items" aria-live="polite">
				{#each preparing.items as item (item.kind + ':' + item.name)}
					<li>
						<span class="chip small">{t(`power.kind.${item.kind}`)}</span>
						<span class="name mono">{item.name}</span>
						<span
							class="state"
							class:ok={item.state === 'stopped'}
							class:bad={item.state === 'failed'}
							title={item.error}>{t(`power.state.${item.state}`)}</span
						>
					</li>
				{/each}
			</ul>
		{:else if preparing}
			<p class="muted">{t('power.preparingNothing')}</p>
		{/if}
		{#if preparing?.phase === 'waiting'}
			<p class="notice-warn" role="alert">
				{t('power.stuck', { names: stuck.map((item) => item.name).join(', ') })}
			</p>
		{/if}
		{#if preparing?.phase === 'failed'}
			<p class="notice-error" role="alert">
				{t('power.goingFailed', { error: preparing.error ?? '' })}
			</p>
		{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<p class="muted small">{t('power.callOffHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={cancelWaiting}>{t('power.callOff')}</button>
			<button
				type="button"
				class="btn btn-danger"
				onclick={anyway}
				disabled={preparing?.phase === 'going'}
			>
				{t(restart ? 'power.anywayRestart' : 'power.anywayShutdown')}
			</button>
		</div>
	{:else if phase !== 'done'}
		{#if status?.pending}
			<div class="notice-warn" role="status">
				{t(status.pending.action === 'reboot' ? 'power.pendingRestart' : 'power.pendingShutdown', {
					user: status.pending.by,
				})}
				<button type="button" class="btn small" onclick={cancelWaiting}
					>{t('power.cancelWaiting')}</button
				>
			</div>
		{/if}
		<ul>
			<li><span class="dot" aria-hidden="true"></span>{t('power.servicesNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.workloadsNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.maintenanceNote')}</li>
			<li><span class="dot" aria-hidden="true"></span>{t('power.diskTestNote')}</li>
			{#if action === 'shutdown'}<li class="muted">
					<span class="dot" aria-hidden="true"></span>{t('power.wakeOnLanNote')}
				</li>{/if}
			<li class="muted">
				<span class="dot" aria-hidden="true"></span>{t(
					action === 'restart' ? 'power.restartNote' : 'power.shutdownNote',
				)}
			</li>
		</ul>
		{#if status?.rebuilding}
			<label class="check"
				><input type="checkbox" bind:checked={afterRebuild} />{t('power.afterRebuild')}</label
			>
		{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={phase === 'working'}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" onclick={confirm} disabled={phase === 'working'}>
				{t(action === 'restart' ? 'power.restartAction' : 'power.shutdownAction')}
			</button>
		</div>
	{:else}
		<p class="muted" role="status">{t(restart ? 'power.restartingText' : 'power.shutDownText')}</p>
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
	.items {
		gap: 6px;
		max-height: 18rem;
		overflow-y: auto;
	}
	.items li {
		align-items: center;
	}
	.items .name {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.state {
		color: var(--muted);
		font-size: 0.8125rem;
	}
	.state.ok {
		color: var(--ok-text);
	}
	.state.bad {
		color: var(--error-text);
	}
	.small {
		font-size: 0.8125rem;
	}
</style>
