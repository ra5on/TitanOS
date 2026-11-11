<script lang="ts">
	// The section "Connections" of the VM form: the network, devices passed
	// through from the host and folders of the NAS shared with the VM.
	import type { Hardware, Share, VM, VMStatus } from '#lib/api.ts';
	import NetworkBridgeSetup from '#lib/components/NetworkBridgeSetup.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import RetryNotice from '#lib/components/RetryNotice.svelte';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import { folderProblem, type Folder } from './vm-form.ts';

	let {
		status,
		hardware,
		shares,
		vmName,
		template,
		running,
		hardwareError,
		sharesError,
		onretryhardware,
		onretryshares,
		onbridgestate,
		net = $bindable(),
		pci = $bindable(),
		usb = $bindable(),
		autoGPU = $bindable(),
		folders = $bindable(),
	}: {
		status: VMStatus | null;
		hardware: Hardware | null;
		shares: Share[];
		/** The VM being edited, whose own devices do not count as taken. */
		vmName?: string;
		template: string;
		running: boolean;
		hardwareError?: string;
		sharesError?: string;
		onretryhardware: () => void;
		onretryshares: () => void;
		onbridgestate: (ready: boolean, pending: boolean) => void;
		net: VM['network'];
		pci: string[];
		usb: string[];
		autoGPU: boolean;
		folders: Folder[];
	} = $props();

	const usedElsewhere = (id: string) => (hardware?.usedBy[id] ?? []).filter((vm) => vm !== vmName);
	const toggle = (list: string[], id: string) =>
		list.includes(id) ? list.filter((entry) => entry !== id) : [...list, id];
	function togglePCI(address: string, companions: string[]) {
		// Removing a card also removes its dependent selections. Adding it
		// never selects another function on the owner's behalf.
		pci = pci.includes(address)
			? pci.filter((id) => id !== address && !companions.includes(id))
			: [...pci, address];
	}
</script>

{#snippet noBridge()}
	{t('vms.form.netNoBridge')}
	<a href="/settings/network">{t('netcfg.title')}</a>
{/snippet}

<fieldset class="srows" disabled={running}>
	<FormRow label={t('vms.form.network')} id="vm-net" more={!status?.bridge ? noBridge : undefined}>
		<select id="vm-net" class="field" bind:value={net}>
			<option value="user">{t('vms.form.netUser')}</option>
			<option value="bridge" disabled={!status?.bridge}>{t('vms.form.netBridge')}</option>
			{#each status?.networks?.filter((n) => n.bridge) ?? [] as n}<option value={`network:${n.id}`}
					>{n.name}{n.vlan ? ` · VLAN ${n.vlan}` : ''}</option
				>{/each}
		</select>
	</FormRow>

	{#if !running}<NetworkBridgeSetup onstate={onbridgestate} />{/if}
</fieldset>
<!-- The two rows open also while the VM runs; their fields are locked then. -->
<SettingRow
	id="vm-devices"
	title={t('vms.form.step4')}
	summary={String(pci.length + usb.length)}
	open={pci.length > 0 || usb.length > 0 || autoGPU}
>
	<fieldset class="sparts" disabled={running}>
		<div class="srow-block">
			<p class="muted note">{t('vms.form.explicitDevices')}</p>
			{#if hardwareError}<RetryNotice text={hardwareError} onretry={onretryhardware} />{/if}
			{#if hardware && !hardware.hardware.iommu && hardware.hardware.pci.length > 0}<p
					class="notice-warn"
				>
					{t('hardware.noIommu')}
				</p>{/if}
		</div>
		{#each hardware?.hardware.pci ?? [] as device (device.address)}
			{@const elsewhere = usedElsewhere(device.address)}
			<SettingSub
				title="{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}"
				for="vm-pci-{device.address}"
			>
				{#snippet description()}
					<span class="mono ids"
						>{device.address} · {device.vendor}:{device.device}{device.companions.length > 0
							? ` · ${t('hardware.companions', { list: device.companions.join(', ') })}`
							: ''}</span
					>
					{#if !device.passable}
						<small>{tOr(`hardware.reason.${device.reason}`, device.reason)}</small>
					{:else}
						{#if device.primary}<small class="warn">{t('hardware.warnPrimary')}</small>{/if}
						{#if device.kind === 'igpu'}<small class="warn">{t('hardware.warnIgpu')}</small>{/if}
						{#if elsewhere.length > 0}<small
								>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small
							>{/if}
					{/if}
				{/snippet}
				{#snippet control()}
					<input
						id="vm-pci-{device.address}"
						class="switch"
						type="checkbox"
						role="switch"
						checked={pci.includes(device.address)}
						disabled={!device.passable}
						onchange={() => togglePCI(device.address, device.companions)}
					/>
				{/snippet}
			</SettingSub>
			{#if pci.includes(device.address)}
				{#each device.companions as address (address)}
					<SettingSub
						title={t('vms.form.companion', { address })}
						desc={t('vms.form.requiredCompanions', { list: device.companions.join(', ') })}
						for="vm-pci-{address}"
					>
						{#snippet control()}
							<input
								id="vm-pci-{address}"
								class="switch"
								type="checkbox"
								role="switch"
								checked={pci.includes(address)}
								onchange={() => (pci = toggle(pci, address))}
							/>
						{/snippet}
					</SettingSub>
				{/each}
			{/if}
		{/each}
		{#each hardware?.hardware.usb ?? [] as device (device.port)}
			{@const elsewhere = usedElsewhere(device.id)}
			<SettingSub title="USB · {device.name || device.id}" for="vm-usb-{device.port}">
				{#snippet description()}
					<span class="mono ids">{device.id} · {t('hardware.port', { port: device.port })}</span>
					{#if device.stick}<small>{t('hardware.stick')}</small>{/if}
					{#if device.duplicate}<small class="warn">{t('hardware.duplicate')}</small>{/if}
					{#if elsewhere.length > 0}<small
							>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small
						>{/if}
				{/snippet}
				{#snippet control()}
					<input
						id="vm-usb-{device.port}"
						class="switch"
						type="checkbox"
						role="switch"
						checked={usb.includes(device.id)}
						disabled={device.stick}
						onchange={() => (usb = toggle(usb, device.id))}
					/>
				{/snippet}
			</SettingSub>
		{/each}
		{#if hardware && hardware.hardware.pci.length === 0 && hardware.hardware.usb.length === 0}
			<div class="srow-block"><p class="muted note">{t('vms.form.noDevices')}</p></div>
		{/if}
		<SettingSub title={t('hardware.autoGPU')} desc={t('hardware.autoGPUHint')} for="vm-autogpu">
			{#snippet control()}
				<input
					id="vm-autogpu"
					class="switch"
					type="checkbox"
					role="switch"
					bind:checked={autoGPU}
				/>
			{/snippet}
		</SettingSub>
		<div class="srow-block">
			{#if pci.length > 0}<small>{t('vms.form.passNote')}</small>{/if}
			<a href="/settings/hardware">{t('vms.form.hardwarePage')}</a>
		</div>
	</fieldset>
</SettingRow>
<SettingRow
	id="vm-folders"
	title={t('vms.form.step5')}
	summary={String(folders.length)}
	open={folders.length > 0}
>
	<fieldset class="sparts" disabled={running}>
		<div class="srow-block">
			{#if sharesError}<RetryNotice text={sharesError} onretry={onretryshares} />{/if}
			<datalist id="vm-shares"
				>{#each shares as share (share.name)}<option value={share.path}></option>{/each}</datalist
			>
			{#each folders as folder, index (index)}
				{@const issue = folder.path || folder.tag ? folderProblem(folders, folder, index) : ''}
				<div class="folder">
					<input
						class="field mono path-field"
						type="text"
						list="vm-shares"
						bind:value={folder.path}
						placeholder="/mnt/…"
						aria-label={t('vms.form.folderOnNas')}
						spellcheck="false"
					/>
					<span class="muted" aria-hidden="true">→</span>
					<input
						class="field mono tag-field"
						type="text"
						bind:value={folder.tag}
						placeholder={t('vms.form.folderName')}
						aria-label={t('vms.form.folderInVm')}
						autocapitalize="none"
						spellcheck="false"
					/>
					<select
						class="field ro-field"
						value={folder.readOnly ? 'ro' : 'rw'}
						onchange={(event) => (folder.readOnly = event.currentTarget.value === 'ro')}
						aria-label={t('vms.form.folderAccess')}
					>
						<option value="rw">{t('editor.readWrite')}</option>
						<option value="ro">{t('editor.readOnly')}</option>
					</select>
					<button
						type="button"
						class="btn"
						aria-label={t('vms.form.folderRemove')}
						onclick={() => folders.splice(index, 1)}>✕</button
					>
					{#if issue}<small class="warn wide-note">{issue}</small>{/if}
				</div>
			{:else}
				<p class="muted note">{t('vms.form.noFolders')}</p>
			{/each}
			<div>
				<button
					type="button"
					class="btn"
					onclick={() =>
						folders.push({
							path: shares[0]?.path ?? '',
							tag: shares[0]?.name ?? '',
							readOnly: false,
						})}
					disabled={folders.length >= 8}>+ {t('vms.form.addFolder')}</button
				>
			</div>
			{#if folders.length > 0}
				<small
					>{t(template === 'windows' ? 'vms.form.folderWindows' : 'vms.form.folderLinux')}</small
				>
				{#if template !== 'windows'}<span class="mono path"
						>mount -t virtiofs {folders[0].tag || '<name>'} /mnt</span
					>{/if}
			{/if}
		</div>
	</fieldset>
</SettingRow>

<style>
	small {
		display: block;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.note {
		font-size: var(--text-base);
	}
	.path {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.ids {
		display: block;
	}
	.folder {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.path-field {
		flex: 2 1 180px;
		width: auto;
	}
	.tag-field {
		flex: 1 1 110px;
		width: auto;
	}
	.ro-field {
		flex: 0 1 170px;
		width: auto;
	}
	.wide-note {
		flex: 1 1 100%;
	}
</style>
