<script lang="ts">
	// The main actions of the chosen VM in the tool bar – the screen as the
	// main one while the VM runs – and the rest in "⋯" (PLAN 67).
	import type { VM } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import TasksPanel from '#lib/components/TasksPanel.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addVmConsole } from '#lib/terminal.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import type { VmsPage } from './vms-page.svelte.ts';

	let { p, current, tasks = true }: { p: VmsPage; current: VM; tasks?: boolean } = $props();
</script>

<!-- PLAN 67: the main actions, the screen as the main one while the VM
     runs; the rest in "⋯" (what does not fit moves there too). -->
{#if tasks}<TasksPanel compact />{/if}
{#if current.state === 'running'}
	<button
		type="button"
		class="btn"
		onclick={() => p.act(current, 'shutdown')}
		disabled={p.isBusy(current)}><Icon name="power" size={15} />{t('vms.shutdown')}</button
	>
	<button
		type="button"
		class="btn"
		onclick={() => p.act(current, 'reboot')}
		disabled={p.isBusy(current)}><Icon name="restart" size={15} />{t('vms.reboot')}</button
	>
	{#if p.tab !== 'screen'}<a
			class="btn btn-primary"
			href={p.link(current, 'screen')}
			data-sveltekit-replacestate
			data-sveltekit-noscroll>{t('vms.openScreen')}</a
		>{/if}
{:else}
	<button
		type="button"
		class="btn btn-primary"
		onclick={() => p.act(current, 'start')}
		disabled={p.isBusy(current) || !p.canStart(current)}>{t('vms.start')}</button
	>
{/if}
<Dropdown label={t('ui.more')} icon="more" align="right">
	{#snippet children(close)}
		<a role="menuitem" href="/vms/edit?vm={encodeURIComponent(current.name)}" onclick={close}
			><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></a
		>
		<hr />
		{#if current.state === 'running'}
			<button
				type="button"
				role="menuitem"
				disabled={p.isBusy(current)}
				onclick={() => {
					close();
					p.open('poweroff');
				}}><Icon name="power" size={16} /><span>{t('vms.poweroff')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					close();
					addVmConsole(current.name, vmLabel(current));
				}}><Icon name="terminal" size={16} /><span>{t('vms.serial')}</span></button
			>
			<hr />
		{/if}
		<button
			type="button"
			role="menuitem"
			onclick={() => {
				close();
				p.showXML();
			}}><Icon name="fileText" size={16} /><span>{t('vms.showXml')}</span></button
		>
		{#if current.state !== 'running'}
			<button
				type="button"
				role="menuitem"
				disabled={p.isBusy(current) || !!current.job}
				onclick={() => {
					close();
					p.cloning = current;
				}}><Icon name="copy" size={16} /><span>{t('ops.clone')}</span></button
			>
		{/if}
	{/snippet}
</Dropdown>
