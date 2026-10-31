<script lang="ts" module>
	export type RowChoice =
		| 'shutdown'
		| 'reboot'
		| 'screen'
		| 'start'
		| 'edit'
		| 'clone'
		| 'snapshots'
		| 'poweroff'
		| 'remove';
</script>

<script lang="ts">
	// The menu of a VM in the list: at the mouse or below its "…" button.
	import type { VM } from '#lib/api.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';

	type Props = {
		vm: VM;
		x: number;
		y: number;
		busy: boolean;
		canStart: boolean;
		onclose: () => void;
		onchoose: (choice: RowChoice) => void;
	};
	let { vm, x, y, busy, canStart, onclose, onchoose }: Props = $props();
</script>

<ContextMenu label={t('vms.actionsFor', { name: vmLabel(vm) })} {x} {y} {onclose}>
	{#if vm.state === 'running'}
		<button type="button" role="menuitem" disabled={busy} onclick={() => onchoose('shutdown')}
			><Icon name="stop" size={16} /><span>{t('vms.shutdown')}</span></button
		>
		<button type="button" role="menuitem" disabled={busy} onclick={() => onchoose('reboot')}
			><Icon name="restart" size={16} /><span>{t('vms.reboot')}</span></button
		>
		<button type="button" role="menuitem" onclick={() => onchoose('screen')}
			><Icon name="monitor" size={16} /><span>{t('vms.tab.screen')}</span></button
		>
	{:else}
		<button
			type="button"
			role="menuitem"
			disabled={busy || !canStart}
			onclick={() => onchoose('start')}
			><Icon name="play" size={16} /><span>{t('vms.start')}</span></button
		>
	{/if}
	<button type="button" role="menuitem" onclick={() => onchoose('edit')}
		><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></button
	>
	{#if vm.state === 'stopped'}<button
			type="button"
			role="menuitem"
			disabled={busy || !!vm.job}
			onclick={() => onchoose('clone')}
			><Icon name="copy" size={16} /><span>{t('ops.clone')}</span></button
		>{/if}
	<button type="button" role="menuitem" onclick={() => onchoose('snapshots')}
		><Icon name="camera" size={16} /><span>{t('snapshots.title')}</span></button
	>
	<hr />
	{#if vm.state === 'running'}
		<button
			type="button"
			role="menuitem"
			class="danger"
			disabled={busy}
			onclick={() => onchoose('poweroff')}
			><Icon name="power" size={16} /><span>{t('vms.poweroff')}</span></button
		>
	{:else}
		<button
			type="button"
			role="menuitem"
			class="danger"
			disabled={busy}
			onclick={() => onchoose('remove')}
			><Icon name="trash" size={16} /><span>{t('vms.remove')}</span></button
		>
	{/if}
</ContextMenu>
