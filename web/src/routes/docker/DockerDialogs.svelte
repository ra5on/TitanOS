<script lang="ts">
	// The page's dialogs and the menu of a row.
	import Dialog from '#lib/components/Dialog.svelte';
	import { inWindow } from '#lib/desktop/mode.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import AutoUpdateDialog from './AutoUpdateDialog.svelte';
	import ComposeSettingsDialog from './ComposeSettingsDialog.svelte';
	import type { DockerPage } from './docker-page.svelte.ts';
	import LogsDialog from './LogsDialog.svelte';
	import RemoveAppDialog from './RemoveAppDialog.svelte';
	import RowMenu from './RowMenu.svelte';
	import TailnetDialog from './TailnetDialog.svelte';
	import UpdatesDialog from './UpdatesDialog.svelte';

	let { p }: { p: DockerPage } = $props();
</script>

{#if p.confirmStopAll}
	<Dialog title={t('docker.stopAllTitle')} onclose={() => (p.confirmStopAll = false)}>
		<p>{t('docker.stopAllText', { count: p.runCount })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (p.confirmStopAll = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" onclick={p.stopAll}>{t('docker.stopAll')}</button>
		</div>
	</Dialog>
{/if}

{#if p.rowMenu}
	{@const group = p.rowMenu.group}
	{@const app = p.groupApp(group)}
	<RowMenu
		{group}
		x={p.rowMenu.x}
		y={p.rowMenu.y}
		{app}
		busy={p.groupBusy(group)}
		update={p.groupUpdate(group)}
		inTailnet={!!app && p.appInTailnet(app)}
		primaryServices={p.layout.primaryServices}
		host={p.host}
		tailscaleHost={p.tailscaleHost}
		{inWindow}
		proxy={!!p.proxy}
		savingLayout={p.savingLayout}
		onclose={() => (p.rowMenu = null)}
		on={{
			restart: () => p.pick(() => p.groupAction(group, 'restart')),
			openRemote: (a) => p.pick(() => p.openRemote(a)),
			logs: (c) => p.pick(() => (p.dialog = { kind: 'logs', container: c })),
			console: (c) => p.pick(() => addContainerConsole(c.id, dockerName(c, 'distinct'))),
			update: (a) => p.pick(() => p.updateApp(a)),
			editLinks: () => p.pick(() => p.editLinks(group)),
			tailnet: (a) => p.pick(() => p.openTailnet(a)),
			remove: () => p.pick(() => p.removeGroup(group)),
		}}
	/>
{/if}

{#if p.linkGroup}
	<Dialog
		title={t('docker.stackLinksTitle', { name: p.linkGroup.name })}
		locked={p.savingLayout}
		onclose={() => (p.linkGroup = null)}
	>
		<p class="text">{t('docker.stackLinksHint')}</p>
		<label class="field-row"
			><span>{t('docker.primaryService')}</span><select
				class="field"
				bind:value={p.primaryService}
				disabled={p.savingLayout}
			>
				<option value="">{t('docker.primaryAuto')}</option>
				{#if p.primaryService && !p.linkGroup.members.some((c) => c.service === p.primaryService)}<option
						value={p.primaryService}>{p.primaryService} · {t('docker.primaryUnavailable')}</option
					>{/if}
				{#each [...new Set(p.linkGroup.members
							.map((c) => c.service)
							.filter(Boolean))] as service}<option value={service}>{service}</option>{/each}
			</select></label
		>
		{#if p.layoutError}<p class="notice-error" role="alert">{p.layoutError}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={p.savingLayout} onclick={() => (p.linkGroup = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-primary" disabled={p.savingLayout} onclick={p.saveLinks}
				>{t('common.save')}</button
			>
		</div>
	</Dialog>
{/if}
{#if p.dialog?.kind === 'logs'}
	<LogsDialog container={p.dialog.container} onclose={() => (p.dialog = null)} />
{:else if p.dialog?.kind === 'updates'}
	{@const target = p.dialog}
	<UpdatesDialog
		apps={p.updateApps}
		selected={target.selected}
		failed={target.failed}
		busy={p.dialogBusy}
		onselect={(selected) => (target.selected = selected)}
		onclose={() => (p.dialog = null)}
		onsubmit={p.applyUpdates}
	/>
{:else if p.dialog?.kind === 'remove'}
	{@const container = p.dialog.container}
	<Dialog
		title={t('docker.removeTitle', { name: container.name })}
		locked={p.dialogBusy}
		onclose={() => (p.dialog = null)}
	>
		<p class="text">{t('docker.removeContainer')}</p>
		{#if p.dialogError}<p class="notice-error" role="alert">{p.dialogError.text}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={p.dialogBusy} onclick={() => (p.dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={p.dialogBusy}
				onclick={() => p.remove(container)}>{t('docker.removeSubmit')}</button
			>
		</div>
	</Dialog>
{:else if p.dialog?.kind === 'removeApp'}
	{@const app = p.dialog.app}
	<RemoveAppDialog
		{app}
		volume={p.status?.settings.volume ?? ''}
		busy={p.dialogBusy}
		error={p.dialogError?.text ?? ''}
		onclose={() => (p.dialog = null)}
		onremove={(purge) => p.removeApp(app, purge)}
	/>
{:else if p.dialog}
	<ComposeSettingsDialog
		kind={p.dialog.kind}
		bind:draft={p.draft}
		volumes={p.volumes}
		appdata={`/mnt/${p.status?.settings.volume}/appdata`}
		busy={p.dialogBusy}
		error={p.dialogError}
		onclose={() => (p.dialog = null)}
		onsubmit={p.submit}
	/>
{/if}

{#if p.tailnetDialog}<TailnetDialog
		app={p.tailnetDialog.app}
		mode={p.tailnetDialog.mode}
		port={p.tailnetDialog.port}
		onclose={() => (p.tailnetDialog = null)}
		onchanged={() => {
			void p.loadProxy();
			void p.load();
		}}
	/>{/if}
{#if p.autoUpdates}<AutoUpdateDialog
		apps={[...new Set(p.containers.filter((c) => c.managed && c.app).map((c) => c.app))].sort()}
		onclose={() => (p.autoUpdates = false)}
	/>{/if}

<style>
	.field-row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-md);
	}
	.text {
		font-size: var(--text-md);
	}
</style>
