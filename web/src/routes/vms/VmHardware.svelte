<script lang="ts">
	// §121 (owner 28.11.2026, draft Entwuerfe/121-desktop/3-vm-hardware): the
	// VM's hardware set directly in its rows instead of through "Bearbeiten" –
	// processor and memory with − / +, the first disk grows after a click on
	// "Vergrößern", autostart as a switch, the network as two choices.
	// Firmware and the disk's connection were fixed when the VM was made
	// (docs/rules/VMS-LXC.md) and are shown, not changed.
	//
	// PUT /vms/<name> replaces every setting of the VM, so each change sends
	// all of them as they are (folders, devices, pinning stay as they were).
	import { api, ApiError, type VM, type VMStatus } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatBytes, GB, MIB } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let {
		vm,
		host,
		bridge,
		busy,
		onchanged,
	}: {
		vm: VM;
		host: VMStatus['host'] | undefined;
		bridge: boolean;
		busy: boolean;
		onchanged: () => void;
	} = $props();

	const running = $derived(vm.state === 'running');
	const locked = $derived(running || busy || !!vm.job);
	// The NAS keeps 2 GB for itself; a VM never gets more than the machine has.
	const RESERVE = 2 * GB;
	const maxMiB = $derived(
		host ? Math.max(512, Math.floor((host.memoryBytes - RESERVE) / MIB / 512) * 512) : vm.memoryMiB,
	);
	const maxCPUs = $derived(host?.cpus ?? vm.vcpus);

	let vcpus = $state(0);
	let memoryMiB = $state(0);
	let saving = $state(false);
	let error = $state('');
	// The drafts follow the VM until the account changes them.
	$effect(() => {
		vcpus = vm.vcpus;
		memoryMiB = vm.memoryMiB;
	});

	const diskIndex = $derived(vm.disks.findIndex((d) => !d.path?.startsWith('/dev/') && !d.source));
	const disk = $derived(diskIndex >= 0 ? vm.disks[diskIndex] : undefined);
	let diskBytes = $state(0);
	$effect(() => {
		diskBytes = disk?.sizeBytes ?? 0;
	});
	const DISK_STEP = 10 * GB;

	function settings(change: Partial<Record<string, unknown>>) {
		return {
			description: vm.description,
			autostart: vm.autostart,
			vcpus: vm.vcpus,
			memoryMiB: vm.memoryMiB,
			iso: vm.iso,
			virtioISO: vm.virtioISO ?? '',
			cpuPin: vm.cpuPin ?? [],
			exclusive: vm.exclusive,
			folders: vm.folders ?? [],
			pci: vm.pci ?? [],
			autoGPU: !!vm.autoGPU,
			usb: vm.usb ?? [],
			network: vm.network,
			...change,
		};
	}
	async function save(change: Partial<Record<string, unknown>>) {
		saving = true;
		error = '';
		try {
			await api.put(`/vms/${encodeURIComponent(vm.name)}`, settings(change));
			onchanged();
		} catch (err) {
			error = apiErrorText(err);
			vcpus = vm.vcpus;
			memoryMiB = vm.memoryMiB;
		} finally {
			saving = false;
		}
	}
	// − and + in a row are one change: saved a moment after the last click.
	let timer: ReturnType<typeof setTimeout> | undefined;
	function later(change: Partial<Record<string, unknown>>) {
		clearTimeout(timer);
		timer = setTimeout(() => void save(change), 700);
	}
	$effect(() => () => clearTimeout(timer));

	async function grow() {
		if (!disk || diskBytes <= disk.sizeBytes) return;
		saving = true;
		error = '';
		try {
			await api.post(`/vms/${encodeURIComponent(vm.name)}/grow`, {
				disk: diskIndex,
				sizeBytes: diskBytes,
			});
			onchanged();
		} catch (err) {
			error = err instanceof ApiError ? apiErrorText(err) : String(err);
			diskBytes = disk.sizeBytes;
		} finally {
			saving = false;
		}
	}
	const network = $derived(
		vm.network === 'bridge' ? 'bridge' : vm.network === 'user' ? 'user' : 'own',
	);
</script>

{#snippet stepper(
	label: string,
	value: string,
	down: () => void,
	up: () => void,
	canDown: boolean,
	canUp: boolean,
)}
	<span class="step" role="group" aria-label={label}>
		<button
			type="button"
			class="round"
			aria-label={t('vmhw.less', { name: label })}
			disabled={!canDown}
			onclick={down}><Icon name="minus" size={16} /></button
		>
		<output class="value" aria-live="polite">{value}</output>
		<button
			type="button"
			class="round"
			aria-label={t('vmhw.more', { name: label })}
			disabled={!canUp}
			onclick={up}><Icon name="plus" size={16} /></button
		>
	</span>
{/snippet}

<div class="hw">
	<SettingGroup title={t('vmhw.power')} id="hw-power">
		<SettingRow icon="chip" title={t('vms.ov.cpus')} desc={t('vmhw.cpusHint', { count: maxCPUs })}>
			{#snippet control()}{@render stepper(
					t('vms.ov.cpus'),
					t('vmhw.cores', { count: vcpus }),
					() => {
						vcpus = Math.max(1, vcpus - 1);
						later({ vcpus });
					},
					() => {
						vcpus = Math.min(maxCPUs, vcpus + 1);
						later({ vcpus });
					},
					!locked && vcpus > 1,
					!locked && vcpus < maxCPUs,
				)}{/snippet}
		</SettingRow>
		<SettingRow
			icon="memory"
			title={t('vms.ov.memory')}
			desc={t('vmhw.memoryHint', { max: formatBytes(maxMiB * MIB) })}
		>
			{#snippet control()}{@render stepper(
					t('vms.ov.memory'),
					formatBytes(memoryMiB * MIB),
					() => {
						memoryMiB = Math.max(512, memoryMiB - (memoryMiB > 2048 ? 1024 : 512));
						later({ memoryMiB });
					},
					() => {
						memoryMiB = Math.min(maxMiB, memoryMiB + (memoryMiB >= 2048 ? 1024 : 512));
						later({ memoryMiB });
					},
					!locked && memoryMiB > 512,
					!locked && memoryMiB < maxMiB,
				)}{/snippet}
		</SettingRow>
		{#if disk}
			<SettingRow icon="disk" title={t('vmhw.disk')} desc={t('vmhw.diskHint')}>
				{#snippet control()}
					{@render stepper(
						t('vmhw.disk'),
						formatBytes(diskBytes),
						() => (diskBytes = Math.max(disk!.sizeBytes, diskBytes - DISK_STEP)),
						() => (diskBytes = Math.min(16 * 1024 * GB, diskBytes + DISK_STEP)),
						!locked && diskBytes > disk!.sizeBytes,
						!locked,
					)}
					{#if diskBytes > disk!.sizeBytes}<button
							type="button"
							class="btn btn-primary"
							disabled={saving}
							onclick={grow}>{t('vmhw.grow', { size: formatBytes(diskBytes) })}</button
						>{/if}
				{/snippet}
			</SettingRow>
		{/if}
	</SettingGroup>

	<SettingGroup title={t('vmhw.start')} id="hw-start">
		<SettingRow icon="power" title={t('vms.ov.firmware')} desc={t('vmhw.fixed')}>
			{#snippet control()}<Segments
					compact
					disabled
					label={t('vms.ov.firmware')}
					value={vm.firmware === 'bios' ? 'bios' : 'uefi'}
					options={[
						{ value: 'uefi', label: 'UEFI' },
						{ value: 'bios', label: 'BIOS' },
					]}
				/>{/snippet}
		</SettingRow>
		<SettingRow icon="disk" title={t('vmhw.bus')} desc={t('vmhw.busHint')}>
			{#snippet control()}<Segments
					compact
					disabled
					label={t('vmhw.bus')}
					value={vm.template === 'linux' ? 'virtio' : 'sata'}
					options={[
						{ value: 'virtio', label: 'VirtIO' },
						{ value: 'sata', label: 'SATA' },
					]}
				/>{/snippet}
		</SettingRow>
		<SettingRow
			icon="restart"
			title={t('vms.autostart')}
			desc={t('vmhw.autostartHint')}
			for="hw-autostart"
		>
			{#snippet control()}<input
					id="hw-autostart"
					type="checkbox"
					role="switch"
					class="switch"
					checked={vm.autostart}
					disabled={busy || saving}
					onchange={(e) => void save({ autostart: e.currentTarget.checked })}
				/>{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('vms.ov.network')} id="hw-network">
		<div class="choices" role="radiogroup" aria-label={t('vms.ov.network')}>
			<button
				type="button"
				role="radio"
				class="choice"
				aria-checked={network === 'user'}
				disabled={locked || saving}
				onclick={() => network !== 'user' && void save({ network: 'user' })}
			>
				<span class="radio" aria-hidden="true"></span>
				<span><b>{t('vmhw.private')}</b><small>{t('vmhw.privateHint')}</small></span>
			</button>
			<button
				type="button"
				role="radio"
				class="choice"
				aria-checked={network === 'bridge'}
				disabled={locked || saving || (!bridge && network !== 'bridge')}
				onclick={() => network !== 'bridge' && void save({ network: 'bridge' })}
			>
				<span class="radio" aria-hidden="true"></span>
				<span
					><b>{t('vmhw.home')}</b><small
						>{bridge || network === 'bridge' ? t('vmhw.homeHint') : t('vmhw.noBridge')}</small
					></span
				>
			</button>
			{#if network === 'own'}
				<div class="choice" role="radio" aria-checked="true" aria-disabled="true">
					<span class="radio" aria-hidden="true"></span>
					<span><b>{t('vmhw.own')}</b><small>{vm.network.replace(/^network:/, '')}</small></span>
				</div>
			{/if}
		</div>
	</SettingGroup>
	{#if running}<p class="muted small">{t('vmhw.runningHint')}</p>{/if}
	{#if saving}<p class="muted small" role="status">{t('vmhw.saving')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</div>

<style>
	.hw {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
	}
	.step {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.round {
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.round:disabled {
		opacity: 0.35;
		cursor: default;
	}
	.value {
		min-width: 6.5em;
		padding: 6px 10px;
		border-radius: 9px;
		border: 1px solid var(--border);
		background: var(--bg);
		text-align: center;
		font-weight: 600;
		white-space: nowrap;
	}
	.choices {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.choice {
		display: flex;
		align-items: flex-start;
		gap: 14px;
		width: 100%;
		padding: 12px 16px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.choice:disabled {
		cursor: default;
		opacity: 0.6;
	}
	.choice[aria-checked='true'] {
		border-color: var(--accent);
		opacity: 1;
	}
	.choice > span:last-child {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.choice small {
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.radio {
		flex: none;
		width: 18px;
		height: 18px;
		margin-top: 2px;
		border-radius: 50%;
		border: 2px solid var(--muted);
	}
	.choice[aria-checked='true'] .radio {
		border-color: var(--accent);
		box-shadow: inset 0 0 0 3px var(--surface);
		background: var(--accent);
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
