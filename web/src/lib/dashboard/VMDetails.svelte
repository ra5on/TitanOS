<script lang="ts">
	import { api, type VM } from '#lib/api.ts';
	import { onMount } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	let { vm, onclose }: { vm: VM; onclose: () => void } = $props();
	let addresses = $state<Record<string, string[]>>({});
	onMount(() => {
		const controller = new AbortController(),
			queue = [vm].filter((vm) => vm.state === 'running');
		async function worker() {
			while (queue.length && !controller.signal.aborted) {
				const vm = queue.shift()!;
				try {
					const value = await api.get<{ addresses: string[] }>(
						`/vms/${encodeURIComponent(vm.name)}/addresses`,
						AbortSignal.any([controller.signal, AbortSignal.timeout(10000)]),
					);
					if (!controller.signal.aborted) addresses = { ...addresses, [vm.name]: value.addresses };
				} catch {
					/* No guest reply means unknown, never an invented address. */
				}
			}
		}
		const workers = Math.min(4, queue.length);
		for (let i = 0; i < workers; i++) void worker();
		return () => controller.abort();
	});
</script>

<section class="vm-inline" aria-label={t('dashboard.detailsFor', { name: vmLabel(vm) })}>
	<header>
		<strong>{t('dashboard.showDetails')}</strong><button
			class="close-details"
			aria-label={t('dashboard.closeVmDetails')}
			onclick={onclose}><Icon name="close" size={14} /></button
		><span class="state">{tOr(`vms.state.${vm.state}`, vm.state)}</span>
	</header>
	<dl>
		<dt>{t('dashboard.name')}</dt>
		<dd>{vm.name}</dd>
		<dt>{t('dashboard.guest')}</dt>
		<dd>
			{vm.template === 'windows'
				? 'Windows'
				: vm.template === 'linux'
					? 'Linux'
					: t('dashboard.other')}
		</dd>
		<dt>{t('dashboard.field.cpu')}</dt>
		<dd>{vm.vcpus} vCPU</dd>
		<dt>{t('dashboard.field.memory')}</dt>
		<dd>{formatBytes(vm.memoryMiB * 1024 * 1024)}</dd>
		<dt>{t('dashboard.vmUsage')}</dt>
		<dd>
			{vm.state === 'running'
				? formatPercent(vm.cpuPercent) + ' · ' + formatBytes(vm.memoryBytes)
				: '–'}
		</dd>
		<dt>{t('nav.storage')}</dt>
		<dd>
			{vm.volume || '–'}{#each vm.disks ?? [] as disk}<div>
					{formatBytes(disk.sizeBytes)} · {disk.format}
				</div>{/each}
		</dd>
		<dt>{t('tile.network')}</dt>
		<dd>{vm.network === 'bridge' ? 'Bridge' : 'NAT'} · {vm.mac || '–'}</dd>
		<dt>{t('dashboard.ipAddresses')}</dt>
		<dd>{addresses[vm.name]?.join(', ') || '–'}</dd>
		<dt>{t('dashboard.vmBoot')}</dt>
		<dd>
			{vm.firmware || 'UEFI'} · {t(
				vm.autostart ? 'dashboard.autostartOn' : 'dashboard.autostartOff',
			)}
		</dd>
		<dt>{t('dashboard.vmAvailable')}</dt>
		<dd>{t(vm.available ? 'dashboard.yes' : 'dashboard.no')}</dd>
	</dl>
	<a href="/vms?vm={encodeURIComponent(vm.name)}">{t('dashboard.showDetails')} →</a>
</section>

<style>
	.close-details {
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
		padding: 3px;
		margin-left: auto;
	}
	.vm-inline {
		margin-top: 8px;
	}
	section {
		border: 1px solid var(--border);
		border-radius: 4px;
		min-width: 0;
		overflow: hidden;
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding: 10px 12px;
		background: var(--chip);
		border-bottom: 1px solid var(--border);
	}
	strong {
		font-size: var(--text-base);
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.state {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	dl {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr);
		gap: 7px 12px;
		margin: 0;
		padding: 12px;
		font-size: var(--text-sm);
	}
	dt {
		min-width: 0;
		overflow-wrap: anywhere;
		hyphens: auto;
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
		font-family: 'IBM Plex Mono', monospace;
	}
	a {
		display: block;
		padding: 0 12px 12px;
		font-size: var(--text-sm);
	}
</style>
