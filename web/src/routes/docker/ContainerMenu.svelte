<script lang="ts">
	import type { DockerContainer } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, portLabels, type ContainerActions, type WebLink } from './container.ts';

	type Props = ContainerActions & {
		container: DockerContainer;
		/** Name for the button label: "Actions for …". */
		name: string;
		busy: boolean;
		/** The addresses of the app, opened from the menu (list tiles have no link buttons). */
		links?: WebLink[];
		/** Start/Stop as the first entry (the list tiles have no button for it). */
		startStop?: boolean;
		/** Whether the "⋯" button shows; without it the app icon opens the menu. */
		trigger?: boolean;
	};
	let {
		container,
		name,
		busy,
		onaction,
		onlogs,
		onremove,
		onconsole,
		onupdate,
		ontailnet,
		tailnet = false,
		links = [],
		startStop = false,
		trigger = true,
	}: Props = $props();

	const running = $derived(isRunning(container));
	const ports = $derived(portLabels(container));
	// "nas.local:8096" beside an address, so one sees where it leads.
	function where(href: string): string {
		try {
			return new URL(href).host;
		} catch {
			return '';
		}
	}
	// The menu lies in the browser's top layer, so no tile or list clips it.
	let at = $state<{ x: number; y: number } | null>(null);
	let button: HTMLButtonElement | undefined = $state();
	// A press on the button closes an open menu (outside the menu); the
	// click that follows must not open it again.
	let wasOpen = false;

	function toggle() {
		if (wasOpen || at) {
			wasOpen = false;
			at = null;
			return;
		}
		const rect = button?.getBoundingClientRect();
		if (!rect) return;
		// Right edges aligned like before; the menu keeps itself on screen.
		at = { x: Math.max(8, rect.right - 224), y: rect.bottom + 4 };
	}
	/** A press on another trigger (the app icon): like the button's own. */
	export function press() {
		wasOpen = !!at;
	}
	/** Opens under another trigger, left edges aligned; a second click closes. */
	export function toggleUnder(anchor: HTMLElement) {
		if (wasOpen || at) {
			wasOpen = false;
			at = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		at = { x: rect.left, y: rect.bottom + 4 };
	}
	/** Right click on the row or card: the menu opens at the mouse. */
	export function openAt(event: MouseEvent) {
		event.preventDefault();
		at = { x: event.clientX, y: event.clientY };
	}
	function choose(action: () => void) {
		at = null;
		action();
	}
</script>

{#if trigger}<div class="wrap">
		<button
			type="button"
			class="more"
			bind:this={button}
			aria-label={t('docker.actionsFor', { name })}
			title={t('docker.actionsFor', { name })}
			aria-haspopup="menu"
			aria-expanded={!!at}
			onpointerdown={() => (wasOpen = !!at)}
			onclick={toggle}
		>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
				><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle
					cx="19"
					cy="12"
					r="1.8"
				/></svg
			>
		</button>
	</div>{/if}
{#if at}
	<ContextMenu
		label={t('docker.actionsFor', { name })}
		x={at.x}
		y={at.y}
		onclose={() => (at = null)}
	>
		<div class="menu-title">
			<span class="menu-app-icon"><AppIcon src={container.icon} {name} /></span>{name}
		</div>
		{#if startStop}
			<button
				type="button"
				role="menuitem"
				onclick={() => choose(() => onaction(running ? 'stop' : 'start'))}
				disabled={busy}
				><Icon name={running ? 'stop' : 'play'} size={16} /><span
					>{t(running ? 'docker.stop' : 'docker.start')}</span
				></button
			>
			<hr />
		{/if}
		{#if links.length > 0}
			{#each links as link, index (`${index}:${link.href}`)}<a
					role="menuitem"
					href={link.href}
					target="_blank"
					rel="noreferrer"
					title={link.label}
					onclick={() => (at = null)}
					><Icon name={link.icon} size={16} /><span>{link.text}</span><small class="where"
						>{where(link.href)}</small
					></a
				>{/each}
			<hr />
		{/if}
		<button type="button" role="menuitem" onclick={() => choose(onlogs)}
			><Icon name="fileText" size={16} /><span>{t('docker.logs')}</span></button
		>
		<button type="button" role="menuitem" onclick={() => choose(onconsole)} disabled={!running}
			><Icon name="terminal" size={16} /><span>{t('docker.console')}</span></button
		>
		{#if container.managed}
			<a role="menuitem" href="/docker/apps/{container.app}" onclick={() => (at = null)}
				><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
			>
		{/if}
		<button
			type="button"
			role="menuitem"
			onclick={() => choose(() => onaction('restart'))}
			disabled={busy}><Icon name="restart" size={16} /><span>{t('docker.restart')}</span></button
		>
		{#if container.managed && ontailnet}
			<button type="button" role="menuitem" onclick={() => choose(ontailnet)}
				><Icon name="tailnet" size={16} /><span
					>{t(tailnet ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</span
				></button
			>
		{/if}
		{#if container.managed && container.updateAvailable}
			<button type="button" role="menuitem" onclick={() => choose(onupdate)} disabled={busy}
				><Icon name="download" size={16} /><span>{t('docker.updateNow')}</span></button
			>
		{/if}
		<hr />
		<button type="button" role="menuitem" class="danger" onclick={() => choose(onremove)}
			><Icon name="trash" size={16} /><span>{t('docker.remove')}</span></button
		>
		{#if startStop && ports.length > 0}<p class="menu-title ports">
				{t('docker.column.ports')}: {ports.join(', ')}
			</p>{/if}
	</ContextMenu>
{/if}

<style>
	.menu-app-icon {
		display: inline-flex;
		width: 28px;
		height: 28px;
		vertical-align: middle;
		margin-right: 8px;
	}
	.where {
		color: var(--muted);
		font-size: 0.75rem;
	}
	.ports {
		margin: 4px 0 0;
		white-space: normal;
	}
	.wrap {
		position: relative;
		flex: none;
	}
	.more {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.more:hover,
	.more[aria-expanded='true'] {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}
	@media (max-width: 760px) {
		.more {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
