<script lang="ts">
	// The section "Connections" of the VM form: the network, devices passed
	// through from the host and folders of the NAS shared with the VM.
	import type { Hardware, Share, VM, VMStatus } from '#lib/api.ts';
	import NetworkBridgeSetup from '#lib/components/NetworkBridgeSetup.svelte';
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

<fieldset class="content" disabled={running}>
	<label class="netsel"
		><span>{t('vms.form.network')}</span>
		<select class="field" bind:value={net}>
			<option value="user">{t('vms.form.netUser')}</option>
			<option value="bridge" disabled={!status?.bridge}>{t('vms.form.netBridge')}</option>
			{#each status?.networks?.filter((n) => n.bridge) ?? [] as n}<option value={`network:${n.id}`}
					>{n.name}{n.vlan ? ` · VLAN ${n.vlan}` : ''}</option
				>{/each}
		</select>
		{#if !status?.bridge}<small
				>{t('vms.form.netNoBridge')}
				<a href="/settings/network">{t('netcfg.title')}</a></small
			>{/if}
	</label>

	{#if !running}<NetworkBridgeSetup onstate={onbridgestate} />{/if}
	<details class="advanced" open={pci.length > 0 || usb.length > 0 || autoGPU}>
		<summary>{t('vms.form.step4')} <span class="muted">{pci.length + usb.length}</span></summary>
		<div class="advanced-body">
			<p class="muted note">{t('vms.form.explicitDevices')}</p>
			{#if hardwareError}<RetryNotice text={hardwareError} onretry={onretryhardware} />{/if}
			{#if hardware && !hardware.hardware.iommu && hardware.hardware.pci.length > 0}<p
					class="notice-warn"
				>
					{t('hardware.noIommu')}
				</p>{/if}
			{#each hardware?.hardware.pci ?? [] as device (device.address)}
				{@const elsewhere = usedElsewhere(device.address)}
				<div class="device">
					<label class="check"
						><input
							type="checkbox"
							checked={pci.includes(device.address)}
							disabled={!device.passable}
							onchange={() => togglePCI(device.address, device.companions)}
						/><span>{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}</span
						></label
					>
					<span class="muted mono ids"
						>{device.address} · {device.vendor}:{device.device}{device.companions.length > 0
							? ` · ${t('hardware.companions', { list: device.companions.join(', ') })}`
							: ''}</span
					>
					{#if pci.includes(device.address)}
						{#each device.companions as address (address)}
							<label class="check"
								><input
									type="checkbox"
									checked={pci.includes(address)}
									onchange={() => (pci = toggle(pci, address))}
								/><span>{t('vms.form.companion', { address })}</span></label
							>
						{/each}
						{#if device.companions.length}<small
								>{t('vms.form.requiredCompanions', {
									list: device.companions.join(', '),
								})}</small
							>{/if}
					{/if}
					{#if !device.passable}
						<small>{tOr(`hardware.reason.${device.reason}`, device.reason)}</small>
					{:else}
						{#if device.primary}<small class="warn">{t('hardware.warnPrimary')}</small>{/if}
						{#if device.kind === 'igpu'}<small class="warn">{t('hardware.warnIgpu')}</small>{/if}
						{#if elsewhere.length > 0}<small
								>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small
							>{/if}
					{/if}
				</div>
			{/each}
			{#each hardware?.hardware.usb ?? [] as device (device.port)}
				{@const elsewhere = usedElsewhere(device.id)}
				<div class="device">
					<label class="check"
						><input
							type="checkbox"
							checked={usb.includes(device.id)}
							disabled={device.stick}
							onchange={() => (usb = toggle(usb, device.id))}
						/><span>USB · {device.name || device.id}</span></label
					>
					<span class="muted mono ids"
						>{device.id} · {t('hardware.port', { port: device.port })}</span
					>
					{#if device.stick}<small>{t('hardware.stick')}</small>{/if}
					{#if device.duplicate}<small class="warn">{t('hardware.duplicate')}</small>{/if}
					{#if elsewhere.length > 0}<small
							>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small
						>{/if}
				</div>
			{/each}
			{#if hardware && hardware.hardware.pci.length === 0 && hardware.hardware.usb.length === 0}<p
					class="muted note"
				>
					{t('vms.form.noDevices')}
				</p>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={autoGPU} /><span>{t('hardware.autoGPU')}</span></label
			>
			<p class="muted note">{t('hardware.autoGPUHint')}</p>
			{#if pci.length > 0}<small>{t('vms.form.passNote')}</small>{/if}
			<a href="/settings/hardware">{t('vms.form.hardwarePage')}</a>
		</div>
	</details>
	<details class="advanced" open={folders.length > 0}>
		<summary>{t('vms.form.step5')} <span class="muted">{folders.length}</span></summary>
		<div class="advanced-body">
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
						class="btn small"
						aria-label={t('vms.form.folderRemove')}
						onclick={() => folders.splice(index, 1)}>✕</button
					>
					{#if issue}<small class="warn wide-note">{issue}</small>{/if}
				</div>
			{:else}
				<p class="muted note">{t('vms.form.noFolders')}</p>
			{/each}
			<button
				type="button"
				class="add"
				onclick={() =>
					folders.push({
						path: shares[0]?.path ?? '',
						tag: shares[0]?.name ?? '',
						readOnly: false,
					})}
				disabled={folders.length >= 8}>+ {t('vms.form.addFolder')}</button
			>
			{#if folders.length > 0}
				<small
					>{t(template === 'windows' ? 'vms.form.folderWindows' : 'vms.form.folderLinux')}</small
				>
				{#if template !== 'windows'}<span class="mono path"
						>mount -t virtiofs {folders[0].tag || '<name>'} /mnt</span
					>{/if}
			{/if}
		</div>
	</details>
</fieldset>

<style>
	.advanced {
		border-top: 1px solid var(--panel-border);
		padding-top: 10px;
		min-width: 0;
	}
	summary {
		cursor: pointer;
		font-size: var(--text-base);
	}
	.advanced-body {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 12px;
		min-width: 0;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.note {
		font-size: var(--text-base);
	}
	.small {
		height: 2rem;
		padding: 0 0.625rem;
		font-size: var(--text-base);
	}
	.path {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.add {
		align-self: flex-start;
		height: 2.25rem;
		padding: 0 12px;
		background: transparent;
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	fieldset {
		min-width: 0;
		margin: 0;
		border: 0;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.device {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.ids {
		padding-left: 28px;
		font-size: var(--text-sm);
	}
	.device small {
		padding-left: 28px;
	}
	.folder {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.path-field {
		flex: 2 1 260px;
		width: auto;
	}
	.tag-field {
		flex: 1 1 140px;
		width: auto;
	}
	.ro-field {
		flex: 0 1 200px;
		width: auto;
	}
	.wide-note {
		flex: 1 1 100%;
	}
	.netsel {
		max-width: 100%;
	}
</style>
