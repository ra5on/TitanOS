<script lang="ts">
	import type { Snippet } from 'svelte';
	import { tick } from 'svelte';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { focusFirstItem, moveFocus } from './menu.ts';
	import './menu.css';

	type Props = {
		/** Accessible name of the button and of the panel. */
		label: string;
		/** Visible text next to the icon; without it the button shows only the icon. */
		text?: string;
		icon?: IconName;
		/** A menu of actions (arrow keys move between them) or a small panel with fields. */
		kind?: 'menu' | 'panel';
		align?: 'left' | 'right';
		/** On narrow screens only the icon is shown. */
		compact?: boolean;
		disabled?: boolean;
		children: Snippet<[() => void]>;
	};
	let { label, text, icon, kind = 'menu', align = 'left', compact = false, disabled = false, children }: Props = $props();

	let open = $state(false);
	// Opens upwards when there is no room below, so that the page never grows.
	let up = $state(false);
	let root: HTMLElement | undefined = $state();
	let button: HTMLButtonElement | undefined = $state();
	let panel: HTMLElement | undefined = $state();

	async function toggle() {
		if (open) return close();
		const rect = button?.getBoundingClientRect();
		up = !!rect && window.innerHeight - rect.bottom < 320 && rect.top > 320;
		open = true;
		await tick();
		if (kind === 'menu') focusFirstItem(panel);
	}
	function close(focus = false) {
		open = false;
		if (focus) button?.focus();
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			return close(true);
		}
		if (kind === 'menu') moveFocus(panel, event);
	}
</script>

<svelte:window onpointerdown={(event) => open && !root?.contains(event.target as Node) && close()} />

<div class="dropdown" bind:this={root}>
	<button
		type="button"
		class="btn"
		class:icon-only={!text}
		class:compact={compact && !!icon}
		bind:this={button}
		{disabled}
		aria-label={label}
		title={label}
		aria-haspopup={kind === 'menu' ? 'menu' : 'true'}
		aria-expanded={open}
		onclick={toggle}
	>
		{#if icon}<Icon name={icon} size={16} />{/if}
		{#if text}<span class="text">{text}</span><span class="chevron"><Icon name="chevronDown" size={14} /></span>{/if}
	</button>
	{#if open}
		<div
			class="dropdown-panel"
			class:up
			class:right={align === 'right'}
			class:panel={kind === 'panel'}
			bind:this={panel}
			role={kind === 'menu' ? 'menu' : 'group'}
			aria-label={label}
			tabindex="-1"
			onkeydown={keys}
		>
			{@render children(() => close())}
		</div>
	{/if}
</div>

<style>
	.dropdown {
		position: relative;
		flex: none;
	}
	.icon-only {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.dropdown-panel {
		position: absolute;
		left: 0;
		top: calc(100% + 4px);
		z-index: 40;
		min-width: 14rem;
		max-width: min(22rem, calc(100vw - 32px));
		display: flex;
		flex-direction: column;
		padding: 6px;
		/* Solid: inside a glass tile a second backdrop blur only sees the rows
		   beneath it, and their text would show through. */
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
	}
	.dropdown-panel.right {
		left: auto;
		right: 0;
	}
	.dropdown-panel.up {
		top: auto;
		bottom: calc(100% + 4px);
	}
	.dropdown-panel.panel {
		gap: 10px;
		padding: 12px;
	}
	.chevron {
		display: inline-flex;
	}
	@media (max-width: 760px) {
		.icon-only,
		.compact {
			width: 2.75rem;
			padding: 0;
			justify-content: center;
		}
		.compact .text,
		.compact .chevron {
			display: none;
		}
	}
</style>
