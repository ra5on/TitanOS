<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	type Device = {
		id: string;
		title: string;
		kind: 'igpu' | 'gpu' | 'npu' | 'nvidia' | 'coral';
		vendor?: string;
		available: boolean;
		reason: string;
		warning?: string;
	};
	let {
		selected = $bindable<string[]>([]),
		onchange = () => {},
	}: { selected: string[]; onchange?: () => void } = $props();
	let devices = $state<Device[]>([]);
	let error = $state('');
	onMount(() => {
		api.get<Device[]>('/hardware/container-devices').then(
			(value) => {
				devices = value;
				selected = [
					...new Set(
						selected.map(
							(id) =>
								value.find(
									(d) =>
										d.available &&
										((id === 'gpu-nvidia' && d.kind === 'nvidia') ||
											(id === 'gpu-intel' &&
												d.vendor === '8086' &&
												['igpu', 'gpu'].includes(d.kind)) ||
											(id === 'npu-intel' && d.vendor === '8086' && d.kind === 'npu')),
								)?.id ?? id,
						),
					),
				];
			},
			(err) => (error = apiErrorText(err)),
		);
	});
	function toggle(id: string) {
		selected = selected.includes(id) ? selected.filter((value) => value !== id) : [...selected, id];
		onchange();
	}
</script>

<div class="devices" role="group" aria-label={t('hardware.containerTitle')}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#each devices as device (device.id)}
		{@const on = selected.includes(device.id)}
		<label class="device" class:on
			><input
				type="checkbox"
				checked={on}
				disabled={!device.available && !on}
				onchange={() => toggle(device.id)}
			/>
			<span class="what"
				>{device.title}<small
					>{t(
						device.kind === 'nvidia'
							? 'hardware.kind.gpu'
							: device.kind === 'coral'
								? 'hardware.coralKind'
								: `hardware.kind.${device.kind}`,
					)}{#if device.warning === 'usb_bus'}
						· {t('hardware.coralUSBWarning')}{/if}</small
				></span
			><span class="chip" class:ok={device.available}
				>{device.available
					? t('appform.deviceFree')
					: t(device.reason === 'vm' ? 'hardware.inVm' : 'hardware.noDriver')}</span
			>
		</label>
	{/each}
	{#each selected.filter((id) => !devices.some((device) => device.id === id)) as id (id)}
		<label class="device on"
			><input type="checkbox" checked onchange={() => toggle(id)} /><span class="what mono"
				>{id}</span
			></label
		>
	{/each}
	{#if !devices.length && !selected.length && !error}<p class="muted none">
			{t('hardware.nonePci')}
		</p>{/if}
</div>

<style>
	.devices {
		display: flex;
		flex-direction: column;
		gap: 6px;
		max-width: 40rem;
		min-width: 0;
	}
	.device {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: 10px;
		padding: 8px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
		font-size: var(--text-md);
		cursor: pointer;
	}
	.device.on {
		border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
		background: color-mix(in srgb, var(--accent) 8%, var(--bg));
	}
	.what {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.what small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.none {
		margin: 0;
		padding-top: 0.5rem;
		font-size: var(--text-md);
	}
</style>
