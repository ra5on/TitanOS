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
	let root: HTMLElement | undefined = $state();
	let button: HTMLButtonElement | undefined = $state();
	let panel: HTMLElement | undefined = $state();
	let left = $state(0),
		top = $state(0);

	async function toggle() {
		if (open) return close();
		open = true;
		await tick();
		// The top layer keeps the panel outside clipped tiles, and its glass
		// blurs the page instead of only the tile it was opened from.
		panel?.showPopover();
		place();
		if (kind === 'menu') focusFirstItem(panel);
	}
	// Under the button, or above it when there is no room below; always on screen.
	function place() {
		if (!button || !panel) return;
		const rect = button.getBoundingClientRect();
		const { width, height } = panel.getBoundingClientRect();
		const below = window.innerHeight - rect.bottom;
		const up = below < height + 12 && rect.top > below;
		const start = align === 'right' ? rect.right - width : rect.left;
		left = Math.max(8, Math.min(start, window.innerWidth - width - 8));
		top = up ? Math.max(8, rect.top - 4 - height) : rect.bottom + 4;
	}
	// Follow the button while the page or a list around it scrolls.
	$effect(() => {
		if (!open) return;
		const follow = () => place();
		window.addEventListener('scroll', follow, true);
		window.addEventListener('resize', follow);
		return () => {
			window.removeEventListener('scroll', follow, true);
			window.removeEventListener('resize', follow);
		};
	});
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
			class="dropdown-panel floating-menu"
			class:panel={kind === 'panel'}
			popover="manual"
			style:left="{left}px"
			style:top="{top}px"
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
		position: fixed;
		inset: auto;
		margin: 0;
		min-width: 14rem;
		max-width: min(22rem, calc(100vw - 32px));
		max-height: calc(100dvh - 16px);
		overflow: auto;
		display: flex;
		flex-direction: column;
		padding: 6px;
		/* Surface from .floating-menu (app.css). */
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
