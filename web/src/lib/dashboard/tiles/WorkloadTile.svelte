<script lang="ts">
	import type { VM, DockerContainer } from '#lib/api.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	let {
		kind,
		name,
		vm,
		container,
		ready,
		error,
		admin,
	}: {
		kind: 'vm' | 'container';
		name: string;
		vm?: VM;
		container?: DockerContainer;
		ready: boolean;
		error: string;
		admin: boolean;
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
