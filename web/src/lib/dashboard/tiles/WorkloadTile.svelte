<script lang="ts">
	import type { VM, DockerContainer } from '#lib/api.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import VMDetails from '../VMDetails.svelte';
	import WorkloadMenu from '../WorkloadMenu.svelte';
	import { vmLabel } from '#lib/vm-name.ts';
	let details = $state(false),
		menu = $state<{ x: number; y: number } | null>(null);
	let {
		kind,
		name,
		vm,
		container,
		ready,
		error,
		admin,
		onrefresh = () => {},
	}: {
		kind: 'vm' | 'container';
		name: string;
		vm?: VM;
		container?: DockerContainer;
		ready: boolean;
		error: string;
		admin: boolean;
		onrefresh?: () => void;
	} = $props();
	const item = $derived(kind === 'vm' ? vm : container);
	const running = $derived(item?.state === 'running');
</script>

{#if !admin}
	<p class="muted">{t('dashboard.workloadAdmin')}</p>
{:else if error}
	<p class="notice-warn" role="status">{error}</p>
{:else if !ready}
	<p class="muted">{t('common.loading')}</p>
{:else if !item}
	<p class="muted">{t('dashboard.workloadMissing', { name })}</p>
{:else if kind === 'vm' && vm}
	<button
		class="vm-entry"
		aria-haspopup="menu"
		aria-label={t('dashboard.menu', { name: vmLabel(vm) })}
		onclick={(e) => {
			const r = e.currentTarget.getBoundingClientRect();
			menu = { x: r.left, y: r.bottom };
		}}>{vmLabel(vm)}<span>{tOr(`vms.state.${vm.state}`, vm.state)}</span></button
	>
	<button class="btn small" onclick={() => (details = true)}>{t('dashboard.showDetails')}</button>
	{#if details}<VMDetails vms={[vm]} onclose={() => (details = false)} />{/if}
	{#if menu}<WorkloadMenu
			target={{ kind: 'vm', vm }}
			{...menu}
			host=""
			tailscaleHost=""
			onclose={() => (menu = null)}
			{onrefresh}
			onpin={() => {}}
			oncontainers={() => {}}
		/>{/if}
{:else}
	<div class="usage">
		<span>{t('dashboard.workloadCPU')}</span><strong class="mono"
			>{running ? formatPercent(item.cpuPercent) : '–'}</strong
		>
	</div>
	<div class="usage" title={t(kind === 'vm' ? 'dashboard.hostRAM' : 'dashboard.containerRAM')}>
		<span>{t('dashboard.workloadRAM')}</span><strong class="mono"
			>{running ? formatBytes(item.memoryBytes) : '–'}</strong
		>
	</div>
	{#if !running}<span class="muted state"
			>{tOr(`${kind === 'vm' ? 'vms' : 'docker'}.state.${item.state}`, item.state)}</span
		>{/if}
{/if}

<style>
	.vm-entry {
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		display: flex;
		flex-direction: column;
		gap: 5px;
		overflow-wrap: anywhere;
		cursor: pointer;
	}
	.vm-entry span {
		font-size: var(--tile-label-size, 12px);
		color: var(--muted);
	}
	.small {
		align-self: flex-end;
		font-size: 0.75rem;
		min-height: 30px;
	}
	.usage {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.usage strong {
		font-size: 1.2rem;
		font-weight: 500;
	}
	.state {
		font-size: 0.75rem;
	}
</style>
