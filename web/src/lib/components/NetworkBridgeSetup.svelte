<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Status = {
		config: { bridge: boolean };
		configured: boolean;
		bridgeReady: boolean;
		pending: { bridge: boolean } | null;
		pendingSeconds: number;
		interfaces: { name: string; carrier: boolean }[];
	};
	let {
		auto = false,
		onstate = (_ready: boolean, _pending: boolean) => {},
	}: { auto?: boolean; onstate?: (ready: boolean, pending: boolean) => void } = $props();
	let status = $state<Status | null>(null);
	let busy = $state(false);
	let settling = $state(false);
	let error = $state('');
	let choosePort = $state(false);
	let port = $state('');
	let waiting = $state(false);
	let attempted = false;
	let disposed = false;
	let until = 0;
	const ready = $derived(!!status?.bridgeReady && !status.pending);
	const pending = $derived(busy || waiting || !!status?.pending || (!status && !error));
	$effect(() => onstate(ready, pending));

	async function load() {
		try {
			const res = await api.get<{ network: Status }>('/network');
			if (disposed) return;
			status = res.network;
			waiting = false;
			if (auto && !attempted && !status.configured && !status.config.bridge && !status.pending) {
				attempted = true;
				void enable();
			}
		} catch (err) {
			if (disposed) return;
			if (Date.now() < until) waiting = true;
			else {
				waiting = false;
				error = apiErrorText(err);
			}
		}
	}
	onMount(() => {
		const stop = poll(load, { interval: 2000, skip: () => busy || !(status?.pending || waiting) });
		return () => {
			disposed = true;
			stop();
		};
	});

	async function enable() {
		if (busy || status?.pending) return;
		busy = true;
		error = '';
		until = Date.now() + 150_000;
		// Reconfiguration can interrupt this very HTTP connection. Poll instead
		// of navigating/reloading, keeping the surrounding VM draft in memory.
		const request = api.post<{ network: Status }>('/network/bridge', {
			port,
			onlyUnconfigured: auto,
		});
		try {
			const res = await Promise.race([
				request,
				new Promise<null>((resolve) => setTimeout(() => resolve(null), 8000)),
			]);
			if (disposed) return;
			if (res) {
				status = res.network;
				waiting = false;
				choosePort = false;
			} else {
				waiting = true;
				request.then(
					(res) => {
						if (!disposed) {
							status = res.network;
							waiting = false;
						}
					},
					() => {},
				);
			}
		} catch (err) {
			if (disposed) return;
			if (err instanceof ApiError && err.code === 'network_bridge_port') choosePort = true;
			else if (
				err instanceof ApiError &&
				(err.code === 'unreachable' || err.code === 'network_pending')
			)
				waiting = true;
			else {
				error = apiErrorText(err);
				until = 0;
			}
		} finally {
			if (!disposed) busy = false;
		}
	}
	async function settle(action: 'confirm' | 'revert') {
		settling = true;
		error = '';
		try {
			await api.post(`/network/${action}`, {});
			await load();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			settling = false;
		}
	}
</script>

<div class="bridge-setup">
	{#if ready}
		<p class="ready">{t('bridge.ready')}</p>
	{:else if status?.pending}
		<p role="status">{t('netcfg.pending', { seconds: status.pendingSeconds })}</p>
		<div class="actions">
			<button
				type="button"
				class="btn btn-primary"
				disabled={settling}
				onclick={() => settle('confirm')}>{t('netcfg.keep')}</button
			>
			<button type="button" class="btn" disabled={settling} onclick={() => settle('revert')}
				>{t('netcfg.back')}</button
			>
		</div>
	{:else if pending}
		<p role="status">{t('bridge.connecting')}</p>
	{:else if status && (!auto || !status.configured)}
		<p class="muted">{t('bridge.hint')}</p>
		{#if choosePort}
			<label
				>{t('bridge.port')}<select class="field" bind:value={port}>
					<option value="">{t('bridge.choosePort')}</option>
					{#each status.interfaces as entry (entry.name)}<option value={entry.name}
							>{entry.name}{entry.carrier ? ` · ${t('netcfg.linkUp')}` : ''}</option
						>{/each}
				</select></label
			>
		{/if}
		<button type="button" class="btn" disabled={busy || (choosePort && !port)} onclick={enable}
			>{t('bridge.enable')}</button
		>
	{/if}
	{#if error}<div class="notice-error" role="alert">
			{error} <button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button>
		</div>{/if}
</div>

<style>
	.bridge-setup {
		display: flex;
		flex-direction: column;
		gap: 10px;
		font-size: 0.8125rem;
	}
	p {
		margin: 0;
	}
	.ready {
		color: var(--ok-text);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	button {
		align-self: flex-start;
	}
</style>
