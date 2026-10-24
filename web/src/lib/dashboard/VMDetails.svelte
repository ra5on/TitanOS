<script lang="ts">
	import { api, type VM } from '#lib/api.ts';
	import { onMount } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	let { vms, onclose }: { vms: VM[]; onclose: () => void } = $props();
	let addresses = $state<Record<string, string[]>>({});
	onMount(() => {
		const controller = new AbortController(),
			queue = vms.filter((vm) => vm.state === 'running');
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
		for (let i = 0; i < Math.min(4, queue.length); i++) void worker();
		return () => controller.abort();
	});
</script>

<Dialog title={t('dashboard.allVmDetails')} wide {onclose}>
	<div class="vm-details-grid">
		{#each vms as vm (vm.name)}<section>
				<header>
					<strong>{vmLabel(vm)}</strong><span class="state"
						>{tOr(`vms.state.${vm.state}`, vm.state)}</span
					>
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
			</section>{:else}<p class="muted">{t('dashboard.vmDetailsEmpty')}</p>{/each}
	</div>
	<div class="dialog-actions">
		<button class="btn btn-primary" onclick={onclose}>{t('common.done')}</button>
	</div>
</Dialog>

<style>
	.vm-details-grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 14px;
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
		font-size: 0.85rem;
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.state {
		font-size: 0.75rem;
		color: var(--muted);
	}
	dl {
		display: grid;
		grid-template-columns: 100px minmax(0, 1fr);
		gap: 7px 12px;
		margin: 0;
		padding: 12px;
		font-size: 0.75rem;
	}
	dt {
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
		font-size: 0.75rem;
	}
	@media (max-width: 600px) {
		.vm-details-grid {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
