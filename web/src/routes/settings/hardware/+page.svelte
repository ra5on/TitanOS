<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import IntelGuc from './IntelGuc.svelte';
	import NvidiaPower from './NvidiaPower.svelte';
	import Unbound from './Unbound.svelte';
	import { api, type Hardware } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

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

<PageHead title={t('hardware.title')}>
	<p class="muted">{t('hardware.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if data}
	{#if !data.hardware.iommu}<p class="notice-warn" role="status">{t('hardware.noIommu')}</p>{/if}

	<!-- PLAN 78-1, draft 78: a plain heading per kind, every device a row. -->
	<SettingGroup title={t('hardware.pci')} id="hw-pci" desc={t('hardware.how')}>
		{#each data.hardware.pci as device (device.address)}
			{@const nvidia = device.vendor === '10de' && device.driver === 'nvidia'}
			{@const use =
				users(device.address).length > 0
					? t('hardware.assigned', { vms: users(device.address).join(', ') })
					: device.passable
						? t('hardware.free')
						: ''}
			{#snippet about()}
				<span class="mono"
					>{device.address} · {device.vendor}:{device.device} · {device.iommuGroup >= 0
						? t('hardware.group', { group: device.iommuGroup })
						: t('hardware.noGroup')} · {device.driver === 'vfio-pci'
						? t('hardware.inVm')
						: device.driver
							? t('hardware.driver', { driver: device.driver })
							: t('hardware.noDriver')}</span
				>
				{#if device.companions.length > 0}<span class="mono"
						>{t('hardware.companions', { list: device.companions.join(', ') })}</span
					>{/if}
				{#if !device.passable && !users(device.address).length}<span
						>{tOr(`hardware.reason.${device.reason}`, device.reason)}</span
					>{/if}
			{/snippet}
			{#snippet state()}
				{#if device.primary}<span class="chip">{t('hardware.primary')}</span>{/if}
				<span class="chip" class:ok={device.passable}
					>{device.passable ? t('hardware.passable') : t('hardware.notPassable')}</span
				>
			{/snippet}
			{#if nvidia}
				<SettingRow
					id="pci-{device.address}"
					icon={device.kind === 'npu' ? 'npu' : 'gpu'}
					title="{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}"
					description={about}
					summary={use}
					control={state}
				>
					<NvidiaPower address={device.address} />
				</SettingRow>
			{:else}
				<SettingRow
					icon={device.kind === 'npu' ? 'npu' : 'gpu'}
					title="{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}"
					description={about}
					summary={use}
					control={state}
				/>
			{/if}
		{:else}
			<EmptyState inline title={t('hardware.nonePci')} />
		{/each}
	</SettingGroup>

	<IntelGuc />

	<SettingGroup title={t('hardware.usb')} id="hw-usb">
		{#each data.hardware.usb as device (device.port)}
			<SettingRow
				icon="disc"
				title={device.name || device.id}
				summary={device.stick
					? t('hardware.stick')
					: users(device.id).length > 0
						? t('hardware.assigned', { vms: users(device.id).join(', ') })
						: t('hardware.free')}
			>
				{#snippet description()}<span class="mono"
						>{device.id} · {t('hardware.port', { port: device.port })}</span
					>{/snippet}
				{#snippet control()}{#if device.stick}<span class="chip">{t('hardware.stickChip')}</span
						>{/if}{/snippet}
			</SettingRow>
		{:else}
			<EmptyState inline title={t('hardware.noneUsb')} />
		{/each}
	</SettingGroup>
{/if}

<Unbound />

<style>
	/* Each fact of a device on a line of its own below its name. */
	.mono {
		display: block;
		overflow-wrap: anywhere;
	}
</style>
