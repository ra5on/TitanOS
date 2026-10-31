<script lang="ts">
	// A small "?" next to a technical term. A click opens a short explanation
	// in plain words; a second click, Escape or a click elsewhere closes it.
	// The texts live in the translation files under help.<term>.
	import { tick } from 'svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Props = { term: string };
	let { term }: Props = $props();

	const title = $derived(t(`help.${term}.title` as MessageKey));
	const text = $derived(t(`help.${term}.text` as MessageKey));

	let open = $state(false);
	let button: HTMLButtonElement | undefined = $state();
	let panel: HTMLElement | undefined = $state();
	let left = $state(0),
		top = $state(0);
	const id = $props.id();

	async function toggle() {
		if (open) return close();
		open = true;
		await tick();
		panel?.showPopover();
		place();
	}
	function place() {
		if (!button || !panel) return;
		const rect = button.getBoundingClientRect();
		const { width, height } = panel.getBoundingClientRect();
		const below = window.innerHeight - rect.bottom;
		const up = below < height + 12 && rect.top > below;
		left = Math.max(8, Math.min(rect.left - 24, window.innerWidth - width - 8));
		top = up ? Math.max(8, rect.top - 6 - height) : rect.bottom + 6;
	}
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
</script>

<svelte:window
	onpointerdown={(event) =>
		open &&
		!button?.contains(event.target as Node) &&
		!panel?.contains(event.target as Node) &&
		close()}
	onkeydown={(event) => open && event.key === 'Escape' && close(true)}
/>

<button
	type="button"
	class="help"
	bind:this={button}
	aria-label={t('help.about', { term: title })}
	aria-expanded={open}
	aria-controls={open ? id : undefined}
	onclick={(event) => {
		// Inside labels and summaries the click must not toggle anything else.
		event.preventDefault();
		event.stopPropagation();
		toggle();
	}}>?</button
>
{#if open}
	<div
		{id}
		class="help-panel floating-menu"
		popover="manual"
		role="note"
		style:left="{left}px"
		style:top="{top}px"
		bind:this={panel}
	>
		<strong>{title}</strong>
		<p>{text}</p>
	</div>
{/if}

<style>
	.help {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex: none;
		width: 20px;
		height: 20px;
		margin: 0 0 0 4px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: var(--chip);
		color: var(--muted);
		font: inherit;
		font-size: 0.75rem;
		font-weight: 600;
		line-height: 1;
		vertical-align: middle;
		cursor: pointer;
	}
	.help:hover,
	.help[aria-expanded='true'] {
		color: var(--text);
		border-color: var(--accent);
	}
	.help:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.help-panel {
		position: fixed;
		inset: auto;
		margin: 0;
		width: min(320px, calc(100vw - 16px));
		padding: 12px 14px;
		border-radius: var(--radius-tile);
		color: var(--text);
		font-size: 0.8125rem;
		line-height: 1.45;
	}
	.help-panel p {
		margin: 4px 0 0;
	}
</style>
