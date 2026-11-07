<script lang="ts">
	// The head of a page: the title, a line or two below it and the page's
	// buttons.
	//
	// PLAN 64: inside a window of the desktop the title bar of the window
	// names the page, so the page shows no big heading of its own (it stays
	// for screen readers). Its buttons sit in one compact tool bar right under
	// the title bar, sticky at the top, the main action on the right; what
	// does not fit moves into a "⋯" menu instead of being cut off. The phone's
	// full-screen view does the same, with the page's name at the left of the
	// bar (the bar above it names only the program).
	import type { Snippet } from 'svelte';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { page } from '$app/state';
	import { appForPath } from '#lib/desktop/apps.ts';
	import Dropdown from './Dropdown.svelte';

	type Props = {
		title: string;
		id?: string;
		children?: Snippet;
		actions?: Snippet;
	};
	let { title, id, children, actions }: Props = $props();

	// The browser tab shows the title of the page; the last head that leaves clears it.
	$effect(() => {
		const shown = title;
		pageTitle.value = shown;
		return () => {
			if (pageTitle.value === shown) pageTitle.value = '';
		};
	});

	const phone = $derived(phoneView());
	// On the phone the bar above names the program: the page's name only when it differs.
	const program = $derived(appForPath(page.url.pathname));
	const ownName = $derived(!program || t(program.label) !== title);
	// A bar only with something in it: actions, the page's name on the phone or
	// (in a window) the page's intro. Never an empty strip.
	const hasBar = $derived(!!actions || (phone ? ownName : !!children));

	// Buttons that can move into the "⋯" menu: plain buttons and links of the
	// bar, never the main action, a field, a group or a button with a menu of
	// its own (its menu would open at a hidden button).
	type Hidden = { el: HTMLElement; label: string; disabled: boolean };
	let bar = $state<HTMLElement>();
	let box = $state<HTMLElement>();
	let hidden = $state<Hidden[]>([]);

	function movable(el: Element): el is HTMLElement {
		return (
			el instanceof HTMLElement &&
			el.matches('button.btn, a.btn') &&
			!el.matches('.btn-primary, [aria-haspopup]') &&
			!el.matches('.win-more')
		);
	}
	function labelOf(el: HTMLElement) {
		return (el.textContent ?? '').trim() || el.getAttribute('aria-label') || el.title || '';
	}
	let more = $state<HTMLElement>();
	/** Whether the bar's content needs more room than the bar has, keeping `reserve` px free. */
	function overflows(reserve: number) {
		if (!box) return false;
		const gap = parseFloat(getComputedStyle(box).columnGap) || 0;
		const shown = [...box.children].filter(
			(el): el is HTMLElement =>
				el instanceof HTMLElement && el !== more && el.getBoundingClientRect().width > 0,
		);
		// What grows (a spacer) only takes free room: it needs none.
		const need =
			shown.reduce(
				(sum, el) =>
					sum +
					(parseFloat(getComputedStyle(el).flexGrow) > 0 ? 0 : el.getBoundingClientRect().width),
				0,
			) +
			gap * Math.max(0, shown.length - 1);
		return need > box.clientWidth - reserve + 0.5;
	}
	// Measured in one go (reading sizes lays the page out): no waiting between steps.
	function fit() {
		if (!box) return;
		const candidates = [...box.children].filter(movable).reverse();
		for (const el of candidates) el.removeAttribute('data-win-overflow');
		if (more) more.style.display = 'none';
		const moved: Hidden[] = [];
		// Room for the "⋯" button once something has moved into it.
		const reserve = 40;
		for (const el of candidates) {
			if (!overflows(moved.length ? reserve : 0)) break;
			el.setAttribute('data-win-overflow', '');
			moved.unshift({
				el,
				label: labelOf(el),
				disabled: el.matches(':disabled, [aria-disabled="true"]'),
			});
		}
		if (more) more.style.display = '';
		const same =
			moved.length === hidden.length &&
			moved.every(
				(m, i) =>
					m.el === hidden[i].el && m.label === hidden[i].label && m.disabled === hidden[i].disabled,
			);
		if (!same) hidden = moved;
	}
	/** Changes that come from the "⋯" menu itself do not count. */
	function ownChange(record: MutationRecord) {
		const inMore = (node: Node) =>
			node instanceof Element ? !!node.closest('.win-more') || node.matches('.win-more') : false;
		if (record.target instanceof Element && record.target.closest('.win-more')) return true;
		const nodes = [...record.addedNodes, ...record.removedNodes];
		return nodes.length > 0 && nodes.every(inMore);
	}
	$effect(() => {
		// Without ResizeObserver (tests in jsdom) the bar simply does not fold.
		if (!box || !bar || typeof ResizeObserver === 'undefined') return;
		let frame = 0;
		const later = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(fit);
		};
		const size = new ResizeObserver(later);
		size.observe(bar);
		// Buttons come and go with the page's state; their text changes too.
		const content = new MutationObserver((records) => {
			if (records.some((r) => !ownChange(r))) later();
		});
		content.observe(box, {
			childList: true,
			subtree: true,
			characterData: true,
			attributes: true,
			attributeFilter: ['disabled', 'hidden', 'class'],
		});
		later();
		return () => {
			cancelAnimationFrame(frame);
			size.disconnect();
			content.disconnect();
		};
	});
</script>

{#if !hasBar}
	<h1 class="sr-only" {id}>{title}</h1>
	{#if children}<div class="win-intro alone">{@render children()}</div>{/if}
{:else}
	<div class="win-toolbar" class:phone class:sticky={!!actions || phone} bind:this={bar}>
		{#if phone && ownName}
			<h1 class="win-title" {id}>{title}</h1>
		{:else}
			<h1 class="sr-only" {id}>{title}</h1>
		{/if}
		{#if !actions && !phone && children}<div class="win-lead">{@render children()}</div>{/if}
		{#if actions}
			<div class="win-actions" bind:this={box}>
				{@render actions()}
				{#if hidden.length}
					<span class="win-more" bind:this={more}>
						<Dropdown label={t('ui.more')} icon="more" align="right">
							{#snippet children(close)}
								{#each hidden as item, i (i)}
									<button
										type="button"
										role="menuitem"
										disabled={item.disabled}
										onclick={() => {
											close();
											item.el.click();
										}}>{item.label}</button
									>
								{/each}
							{/snippet}
						</Dropdown>
					</span>
				{/if}
			</div>
		{/if}
	</div>
	{#if (actions || phone) && children}<div class="win-intro">{@render children()}</div>{/if}
{/if}

<style>
	/* PLAN 64: the tool bar of a window (see app.css for the page around it). */
	.win-toolbar {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 49px;
		margin: 0 calc(-1 * var(--page-pad));
		padding: 8px var(--page-pad);
		background: var(--win-toolbar-bg, var(--bg));
		border-bottom: 1px solid var(--border);
		min-width: 0;
	}
	.win-toolbar.sticky {
		position: sticky;
		top: var(--header-height, 0px);
		z-index: 20;
	}
	.win-title {
		flex: none;
		max-width: 45%;
		font-size: var(--text-md);
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.win-lead {
		flex: 1;
		min-width: 0;
		font-size: var(--text-base);
		color: var(--muted);
	}
	.win-lead :global(p) {
		margin: 0;
	}
	.win-actions {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-wrap: nowrap;
		align-items: center;
		justify-content: flex-end;
		gap: 8px;
	}
	/* Compact controls: 32 px high (on the phone they keep their touch size). */
	.win-toolbar:not(.phone) .win-actions :global(:is(.btn, .field, .segmented, select)) {
		min-height: 32px;
		height: 32px;
		font-size: var(--text-base);
		flex-shrink: 0;
	}
	.win-toolbar:not(.phone) .win-actions :global(.btn) {
		padding: 0 12px;
	}
	.win-toolbar:not(.phone) .win-actions :global(.btn.icon-only),
	.win-toolbar:not(.phone) .win-actions :global(.btn.icon-btn) {
		width: 32px;
		padding: 0;
	}
	/* Nothing in the bar is squeezed (a page lets its search give way itself);
	   the main action stays at the right, the "⋯" menu just before it. */
	.win-actions > :global(*) {
		flex-shrink: 0;
	}
	.win-actions :global(.btn-primary),
	.win-actions > :global(:has(.btn-primary)) {
		order: 10;
	}
	.win-toolbar.phone .win-actions :global(.btn) {
		flex-shrink: 0;
	}
	.win-actions :global([data-win-overflow]) {
		display: none !important;
	}
	.win-more {
		order: 9;
		flex: none;
		display: inline-flex;
	}
	.win-intro {
		margin-top: calc(-1 * var(--gap) + 4px);
		font-size: var(--text-base);
		color: var(--muted);
	}
	.win-intro.alone {
		margin-top: 0;
	}
	/* Straight in the page's <main> (a block, no gap to pull back): it sits
	   below the bar, not half under it (PLAN 65: Speicher, Freigaben). */
	:global(main) > .win-intro:not(.alone) {
		margin-top: 8px;
		margin-bottom: var(--gap, 16px);
	}
	.win-intro :global(p) {
		margin: 0;
		max-width: 60rem;
	}
	/* The phone: the page's name and the main action; a second row for the rest. */
	.win-toolbar.phone {
		flex-wrap: wrap;
		min-height: 48px;
	}
	.phone .win-lead {
		flex-basis: 100%;
		order: 5;
	}
</style>
