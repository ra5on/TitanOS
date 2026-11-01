<script lang="ts" module>
	export type NewEntry =
		| { kind: 'path'; host: string; container: string; readOnly: boolean }
		| { kind: 'port'; host: string; container: string; protocol: 'tcp' | 'udp' }
		| { kind: 'variable'; name: string; value: string }
		| { kind: 'label'; name: string; value: string }
		| { kind: 'device'; device: string };
</script>

<script lang="ts">
	// Adds a path, port, variable, label or device to an app. Devices are
	// picked from what the NAS has (GPUs and accelerators are chosen above,
	// in their own section) or typed in as /dev/….
	import Dialog from '#lib/components/Dialog.svelte';
	import { validDevice } from '#lib/compose/template.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PathField from './PathField.svelte';

	type Props = {
		onadd: (entry: NewEntry) => void;
		onclose: () => void;
		/** Shares the folder browser marks. */
		shares?: { name: string; volume: string }[];
	};
	let { onadd, onclose, shares = [] }: Props = $props();

	const KINDS = ['path', 'port', 'variable', 'label', 'device'] as const;
	let kind = $state<NewEntry['kind']>('path');
	let host = $state(''),
		container = $state(''),
		readOnly = $state(false),
		protocol = $state<'tcp' | 'udp'>('tcp'),
		name = $state(''),
		value = $state(''),
		device = $state(''),
		target = $state('');

	const port = (text: string) => /^\d{1,5}$/.test(text) && +text >= 1 && +text <= 65535;
	const problem = $derived.by(() => {
		if (kind === 'path')
			return host.trim() && container.trim().startsWith('/') ? '' : t('appform.addPathHint');
		if (kind === 'port') return port(host) && port(container) ? '' : t('apps.portInvalid');
		if (kind === 'variable')
			return /^[A-Za-z_][A-Za-z0-9_]*$/.test(name) ? '' : t('apps.ownVarInvalid');
		if (kind === 'label') return name.trim() ? '' : t('appform.addLabelHint');
		return validDevice(entryDevice()) ? '' : t('appform.addDeviceHint');
	});
	function entryDevice() {
		return target.trim() ? `${device.trim()}:${target.trim()}` : device.trim();
	}
	function add() {
		if (problem) return;
		if (kind === 'path') onadd({ kind, host: host.trim(), container: container.trim(), readOnly });
		else if (kind === 'port') onadd({ kind, host, container, protocol });
		else if (kind === 'variable' || kind === 'label') onadd({ kind, name: name.trim(), value });
		else onadd({ kind, device: entryDevice() });
	}
</script>

<Dialog title={t('appform.addTitle')} {onclose}>
	<form
		class="add"
		onsubmit={(event) => {
			event.preventDefault();
			add();
		}}
	>
		<div class="kinds" role="radiogroup" aria-label={t('appform.addKind')}>
			{#each KINDS as entry (entry)}
				<button
					type="button"
					role="radio"
					aria-checked={kind === entry}
					onclick={() => (kind = entry)}>{t(`appform.kind.${entry}`)}</button
				>
			{/each}
		</div>
		{#if kind === 'path'}
			<label class="stack"
				><span>{t('appform.hostPath')}</span>
				<PathField bind:value={host} label={t('appform.hostPath')} {shares} /></label
			>
			<label class="stack"
				><span>{t('appform.containerPath')}</span><input
					class="field mono"
					bind:value={container}
					placeholder="/data"
					spellcheck="false"
				/></label
			>
			<label class="check"
				><input type="checkbox" bind:checked={readOnly} />{t('editor.readOnly')}</label
			>
		{:else if kind === 'port'}
			<div class="pair">
				<label class="stack"
					><span>{t('editor.hostPort')}</span><input
						class="field mono"
						inputmode="numeric"
						bind:value={host}
					/></label
				>
				<label class="stack"
					><span>{t('editor.containerPort')}</span><input
						class="field mono"
						inputmode="numeric"
						bind:value={container}
					/></label
				>
				<label class="stack"
					><span>{t('apps.protocol')}</span><select class="field" bind:value={protocol}
						><option value="tcp">TCP</option><option value="udp">UDP</option></select
					></label
				>
			</div>
		{:else if kind === 'device'}
			<label class="stack"
				><span>{t('appform.device')}</span><input
					class="field mono"
					bind:value={device}
					placeholder="/dev/ttyUSB0"
					spellcheck="false"
				/></label
			>
			<label class="stack"
				><span>{t('appform.deviceTarget')}</span><input
					class="field mono"
					bind:value={target}
					placeholder={device || '/dev/…'}
					spellcheck="false"
				/></label
			>
			<p class="muted note">{t('appform.deviceNote')}</p>
		{:else}
			<label class="stack"
				><span>{t('editor.envName')}</span><input
					class="field mono"
					bind:value={name}
					autocapitalize="none"
					spellcheck="false"
				/></label
			>
			<label class="stack"
				><span>{t('editor.envValue')}</span><input
					class="field mono"
					bind:value
					spellcheck="false"
				/></label
			>
		{/if}
		{#if problem}<p class="muted note">{problem}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={!!problem}>{t('appform.add')}</button>
		</div>
	</form>
</Dialog>

<style>
	.add {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.kinds {
		display: inline-flex;
		flex-wrap: wrap;
		align-self: flex-start;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		overflow: hidden;
	}
	.kinds button {
		padding: 7px 12px;
		background: transparent;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.kinds button[aria-checked='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr 7rem;
		gap: var(--space-2);
	}
	.note {
		margin: 0;
		font-size: var(--text-sm);
	}
</style>
