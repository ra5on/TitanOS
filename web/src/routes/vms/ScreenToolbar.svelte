<script lang="ts">
	// The tool bar of the VM's screen: keys to send, the display mode, the
	// list, a screenshot, the serial console, an own window and full screen.
	import type { VM } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { addVmConsole } from '#lib/terminal.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { KEYS, keyLabel, MODES } from './vm-page.ts';
	import type { VmsPage } from './vms-page.svelte.ts';

	let { p, current }: { p: VmsPage; current: VM } = $props();
</script>

<div class="toolbar" role="toolbar" aria-label={t('vms.screen')}>
	{#if current.state === 'running'}
		<Dropdown label={t('vms.send')} text={t('vms.send')} icon="keyboard" compact>
			{#snippet children(close)}
				{#each KEYS as entry (entry.label)}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							p.screen?.sendKeys(entry.keys);
						}}><span>{keyLabel(entry.label, i18n.locale)}</span></button
					>
				{/each}
				<hr />
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						p.open('paste');
					}}><Icon name="paste" size={16} /><span>{t('vms.clipboard')} …</span></button
				>
			{/snippet}
		</Dropdown>
		<Dropdown label={t('vms.display')} text={t(`vms.mode.${p.shownMode}`)} icon="monitor" compact>
			{#snippet children(close)}
				{#each MODES as entry (entry)}
					<button
						type="button"
						role="menuitemradio"
						aria-checked={p.shownMode === entry}
						disabled={entry === 'remote' && current.template !== 'linux'}
						title={p.modeHint(entry)}
						onclick={() => {
							close();
							p.mode = entry;
						}}><span>{t(`vms.mode.${entry}`)}</span></button
					>
				{/each}
			{/snippet}
		</Dropdown>
		<span class="grow"></span>
		{#if !inWindow}<button
				type="button"
				class="tool icon-tool"
				aria-pressed={p.sidebar}
				aria-label={t('vms.sidebar')}
				title={t('vms.sidebar')}
				onclick={() => (p.sidebar = !p.sidebar)}><Icon name="sidebar" size={16} /></button
			>{/if}
		<button
			type="button"
			class="tool icon-tool"
			aria-label={t('vms.screenshot')}
			title={t('vms.screenshot')}
			onclick={p.saveScreenshot}><Icon name="camera" size={16} /></button
		>
		<button
			type="button"
			class="tool icon-tool"
			aria-label={t('vms.serial')}
			title={t('vms.serial')}
			onclick={() => addVmConsole(current.name, vmLabel(current))}
			><Icon name="terminal" size={16} /></button
		>
		<a
			class="tool icon-tool"
			href="/vms/screen?vm={encodeURIComponent(current.name)}"
			target="_blank"
			rel="noopener"
			aria-label={t('vms.ownWindow')}
			title={t('vms.ownWindow')}
			onclick={p.ownWindow}><Icon name="external" size={16} /></a
		>
		<button
			type="button"
			class="tool icon-tool strong"
			aria-label={t('vms.fullscreen')}
			title={t('vms.fullscreen')}
			onclick={() =>
				// PLAN 87: iPhones have no full screen for a page: the screen fills it by itself.
				p.touch ? (p.touchFull = !p.touchFull) : p.size.frame?.requestFullscreen()}
			><Icon name="maximize" size={16} /></button
		>
	{:else}<span class="grow"></span>
		{#if !inWindow}<button
				type="button"
				class="tool icon-tool"
				aria-pressed={p.sidebar}
				aria-label={t('vms.sidebar')}
				title={t('vms.sidebar')}
				onclick={() => (p.sidebar = !p.sidebar)}><Icon name="sidebar" size={16} /></button
			>{/if}
	{/if}
</div>

<style>
	.icon-tool {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon-tool:hover,
	.icon-tool[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
	}
	.toolbar {
		display: flex;
		flex: 1;
		min-width: 0;
		align-items: center;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	.toolbar::before {
		content: '';
		height: 22px;
		width: 1px;
		background: var(--border);
		margin-right: var(--space-2-5);
	}
	.grow {
		flex: 1 1 auto;
	}
	.icon-tool {
		width: 34px;
		height: 34px;
		color: var(--text);
		text-decoration: none;
	}
	.icon-tool.strong {
		background: var(--chip);
		color: var(--accent);
	}
	@media (max-width: 1100px) {
		.toolbar {
			flex: 1 1 100%;
			padding-bottom: var(--space-2);
			flex-wrap: wrap;
		}
		.toolbar::before {
			display: none;
		}
	}
</style>
