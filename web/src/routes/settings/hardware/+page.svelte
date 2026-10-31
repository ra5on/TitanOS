<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import IntelGuc from './IntelGuc.svelte';
	import NvidiaPower from './NvidiaPower.svelte';
	import Unbound from './Unbound.svelte';
	import { api, type Hardware } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';

	let data = $state<Hardware | null>(null);
	let error = $state('');

	$effect(() =>
		poll(
			() =>
				api.get<Hardware>('/hardware').then(
					(res) => (data = res),
					(err) => (error = apiErrorText(err)),
				),
			{ interval: 5000 },
		),
	);

	const users = (id: string) => data?.usedBy[id] ?? [];
</script>

<PageHead title={t('hardware.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('hardware.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if data}
	{#if !data.hardware.iommu}<p class="notice-warn" role="status">{t('hardware.noIommu')}</p>{/if}

	<section class="tile panel" aria-labelledby="hw-pci">
		<h2 class="tile-title" id="hw-pci">{t('hardware.pci')}</h2>
		{#each data.hardware.pci as device (device.address)}
			<div class="device">
				<div class="main">
					<span class="name"
						>{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}</span
					>
					<span class="muted mono"
						>{device.address} · {device.vendor}:{device.device} · {device.iommuGroup >= 0
							? t('hardware.group', { group: device.iommuGroup })
							: t('hardware.noGroup')}</span
					>
					{#if device.companions.length > 0}<span class="muted mono"
							>{t('hardware.companions', { list: device.companions.join(', ') })}</span
						>{/if}
				</div>
				<div class="state">
					<span class="chip" class:ok={device.passable}
						>{device.passable ? t('hardware.passable') : t('hardware.notPassable')}</span
					>
					<span class="muted"
						>{device.driver === 'vfio-pci'
							? t('hardware.inVm')
							: device.driver
								? t('hardware.driver', { driver: device.driver })
								: t('hardware.noDriver')}</span
					>
					{#if device.primary}<span class="chip">{t('hardware.primary')}</span>{/if}
				</div>
				<div class="use">
					{#if device.vendor === '10de' && device.driver === 'nvidia'}<NvidiaPower
							address={device.address}
						/>{/if}
					{#if users(device.address).length > 0}
						<span>{t('hardware.assigned', { vms: users(device.address).join(', ') })}</span>
					{:else if device.passable}
						<span class="muted">{t('hardware.free')}</span>
					{:else}
						<span class="muted">{tOr(`hardware.reason.${device.reason}`, device.reason)}</span>
					{/if}
				</div>
			</div>
		{:else}
			<EmptyState inline title={t('hardware.nonePci')} />
		{/each}
	</section>

	<IntelGuc />

	<section class="tile panel" aria-labelledby="hw-usb">
		<h2 class="tile-title" id="hw-usb">{t('hardware.usb')}</h2>
		{#each data.hardware.usb as device (device.port)}
			<div class="device">
				<div class="main">
					<span class="name">{device.name || device.id}</span>
					<span class="muted mono">{device.id} · {t('hardware.port', { port: device.port })}</span>
				</div>
				<div class="state">
					{#if device.stick}<span class="chip">{t('hardware.stickChip')}</span>{/if}
				</div>
				<div class="use">
					{#if device.stick}
						<span class="muted">{t('hardware.stick')}</span>
					{:else if users(device.id).length > 0}
						<span>{t('hardware.assigned', { vms: users(device.id).join(', ') })}</span>
					{:else}
						<span class="muted">{t('hardware.free')}</span>
					{/if}
				</div>
			</div>
		{:else}
			<EmptyState inline title={t('hardware.noneUsb')} />
		{/each}
	</section>
	<p class="muted hint">{t('hardware.how')}</p>
{/if}

<Unbound />

<style>
	.hint {
		max-width: 60rem;
		font-size: 0.875rem;
	}
	.device {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 20px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.main {
		flex: 2 1 320px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.main .mono {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
	}
	.state {
		flex: 1 1 220px;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		font-size: 0.8125rem;
	}
	.use {
		flex: 1 1 260px;
		font-size: 0.8125rem;
	}
</style>
