<script lang="ts">
	// The section "Installation" of the VM form: the medium the VM starts
	// from, the VirtIO drivers for Windows and whether it starts with the NAS.
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

<fieldset class="content" disabled={running}>
	{#if mediaError}<RetryNotice text={mediaError} onretry={onretrymedia} />{/if}
	<div class="medium">
		<label for="vm-iso">{t('vms.new.medium')}</label>
		<div class="medium-pick">
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
		<small>{t(media.length > 0 ? 'vms.new.mediumHint' : 'vms.new.mediumNone')}</small>
		<a href="/vms/media">{t('media.add')}</a>
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
	</div>
</fieldset>
<div class="start-options">
	<label class="check"
		><input type="checkbox" bind:checked={autostart} /><span>{t('vms.autostartLabel')}</span></label
	>
</div>

<style>
	.content {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
	}
	.start-options {
		padding: 0 16px 16px;
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
	.medium {
		display: flex;
		flex-direction: column;
		gap: 8px;
		font-size: var(--text-base);
	}
	.medium label {
		font-size: var(--text-md);
	}
	.medium-pick {
		display: flex;
		gap: 8px;
	}
	.medium-pick input {
		flex: 1;
		min-width: 0;
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
</style>
