<script lang="ts">
	// The section "Installation" of the VM form: the medium the VM starts
	// from, the VirtIO drivers for Windows and whether it starts with the NAS.
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import RetryNotice from '#lib/components/RetryNotice.svelte';
	import VirtIODrivers from '#lib/components/VirtIODrivers.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import IsoPicker from './IsoPicker.svelte';

	let {
		media,
		template,
		running,
		virtioVolume,
		virtioInitial,
		virtioAuto,
		mediaError,
		onretrymedia,
		iso = $bindable(),
		virtioISO = $bindable(),
		virtioPending = $bindable(),
		autostart = $bindable(),
	}: {
		media: { path: string; sizeBytes: number }[];
		template: string;
		running: boolean;
		/** Where the VirtIO driver image is kept. */
		virtioVolume: string;
		virtioInitial: string;
		virtioAuto: boolean;
		mediaError?: string;
		onretrymedia: () => void;
		iso: string;
		virtioISO: string;
		virtioPending: boolean;
		autostart: boolean;
	} = $props();

	let browsing = $state(false);
</script>

{#snippet mediumMore()}
	{t(media.length > 0 ? 'vms.new.mediumHint' : 'vms.new.mediumNone')}
	<a href="/vms/media">{t('media.add')}</a>
{/snippet}

<fieldset class="srows" disabled={running}>
	{#if mediaError}<RetryNotice text={mediaError} onretry={onretrymedia} />{/if}
	<FormRow label={t('vms.new.medium')} id="vm-iso" more={mediumMore}>
		<div class="line">
			<input
				id="vm-iso"
				class="field mono"
				type="text"
				bind:value={iso}
				placeholder="/mnt/…/….iso"
				spellcheck="false"
			/>
			<button type="button" class="btn" onclick={() => (browsing = true)}
				><Icon name="folder" size={14} />{t('vms.iso.browse')}</button
			>{#if iso}<button
					type="button"
					class="btn"
					onclick={() => (iso = '')}
					aria-label={t('vms.iso.clear')}><Icon name="close" size={14} /></button
				>{/if}
		</div>
	</FormRow>
	{#if browsing}<IsoPicker
			title={t('vms.iso.title')}
			initial={iso}
			submit={t('vms.iso.pick')}
			onpick={(path) => {
				iso = path;
				browsing = false;
			}}
			onclose={() => (browsing = false)}
		/>{/if}
	{#if template === 'windows'}<VirtIODrivers
			volume={virtioVolume}
			initial={virtioInitial}
			defaultAuto={virtioAuto}
			disabled={running}
			onready={(value) => (virtioISO = value)}
			onbusy={(value) => (virtioPending = value)}
		/>{/if}
</fieldset>
<FormRow label={t('vms.autostartLabel')} id="vm-autostart">
	<input id="vm-autostart" class="switch" type="checkbox" role="switch" bind:checked={autostart} />
</FormRow>
