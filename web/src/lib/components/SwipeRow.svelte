<script lang="ts" module>
	export type SwipeAction = {
		id: string;
		label: string;
		icon?: IconName;
		/** Red, for removing (a removal that loses data asks first, where it runs). */
		danger?: boolean;
		run: () => void;
	};
</script>

<script lang="ts">
	// PLAN 87: a row of a list on the phone that is swiped sideways. To the
	// left it shows its actions at its end (a long swipe runs the first one
	// when `fullSwipe` allows it); to the right it runs the leading action (e.g.
	// "gelesen"). The page scrolls up and down as before; a touch at the left
	// edge of the screen belongs to "Zurück" (PhoneGestures).
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';
	import { motionOK } from './sheet.ts';

	type Props = {
		/** Shown at the end of the row after a swipe to the left. */
		actions?: SwipeAction[];
		/** Runs after a swipe to the right. */
		leading?: SwipeAction;
		/** A long swipe to the left runs the first action at once. */
		fullSwipe?: boolean;
		children: Snippet;
	};
	let { actions = [], leading, fullSwipe = false, children }: Props = $props();

	const ACTION = 84;
	let dx = $state(0);
	let open = $state(false);
	let animate = $state(false);
	let width = $state(0);
	let start: { x: number; y: number; id: number; base: number } | null = null;
	let decided: 'x' | 'y' | null = null;

	const room = $derived(actions.length * ACTION);

	function down(event: PointerEvent) {
		if (event.pointerType === 'mouse' || event.clientX < 28) return;
		start = { x: event.clientX, y: event.clientY, id: event.pointerId, base: open ? -room : 0 };
		decided = null;
		animate = false;
	}
	function move(event: PointerEvent) {
		if (!start || event.pointerId !== start.id) return;
		const x = event.clientX - start.x;
		const y = event.clientY - start.y;
		if (!decided) {
			if (Math.abs(x) < 10 && Math.abs(y) < 10) return;
			decided = Math.abs(x) > Math.abs(y) * 1.2 ? 'x' : 'y';
			if (decided === 'x')
				(event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
		}
		if (decided !== 'x') return;
		event.preventDefault();
		const min = actions.length ? -width : 0;
		const max = leading ? ACTION * 1.6 : 0;
		dx = Math.max(min, Math.min(max, start.base + x));
	}
	function up(event: PointerEvent) {
		if (!start || event.pointerId !== start.id) return;
		start = null;
		if (decided !== 'x') return;
		animate = motionOK();
		if (leading && dx > ACTION) {
			dx = 0;
			open = false;
			leading.run();
			return;
		}
		if (fullSwipe && actions[0] && dx < -width * 0.6) {
			dx = 0;
			open = false;
			actions[0].run();
			return;
		}
		open = actions.length > 0 && dx < -room / 2;
		dx = open ? -room : 0;
	}
	function choose(action: SwipeAction) {
		animate = motionOK();
		open = false;
		dx = 0;
		action.run();
	}
	/** A click on the row while its actions show only closes them. */
	function capture(event: MouseEvent) {
		if (!open && decided !== 'x') return;
		if ((event.target as HTMLElement).closest('.swipe-actions')) return;
		event.preventDefault();
		event.stopPropagation();
		decided = null;
		animate = motionOK();
		open = false;
		dx = 0;
	}
</script>

<div class="swipe-row" class:open bind:clientWidth={width} data-swipe-row>
	{#if leading && dx > 0}
		<div class="swipe-leading" aria-hidden="true" class:armed={dx > ACTION}>
			{#if leading.icon}<Icon name={leading.icon} size={18} />{/if}<span>{leading.label}</span>
		</div>
	{/if}
	{#if actions.length && (dx < 0 || open)}
		<div class="swipe-actions" style:width="{Math.max(room, -dx)}px" inert={!open && dx === 0}>
			{#each actions as action (action.id)}
				<button
					type="button"
					class="swipe-action"
					class:danger={action.danger}
					data-swipe-action={action.id}
					onclick={() => choose(action)}
					>{#if action.icon}<Icon name={action.icon} size={18} />{/if}<span>{action.label}</span
					></button
				>
			{/each}
		</div>
	{/if}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="swipe-content"
		class:animate
		style:transform={dx ? `translateX(${dx}px)` : undefined}
		onpointerdown={down}
		onpointermove={move}
		onpointerup={up}
		onpointercancel={up}
		onclickcapture={capture}
	>
		{@render children()}
	</div>
</div>

<style>
	.swipe-row {
		position: relative;
		overflow: hidden;
	}
	.swipe-content {
		position: relative;
		z-index: 1;
		background: var(--swipe-bg, var(--surface));
		touch-action: pan-y;
	}
	.swipe-content.animate {
		transition: transform 0.18s ease-out;
	}
	.swipe-actions {
		position: absolute;
		top: 0;
		right: 0;
		bottom: 0;
		display: flex;
		justify-content: flex-end;
	}
	.swipe-action {
		flex: 1 0 84px;
		min-height: 44px;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		padding: 0 var(--space-2);
		border: 0;
		background: var(--accent);
		color: var(--on-accent);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.swipe-action.danger {
		background: var(--status-error);
		color: var(--on-warn);
	}
	.swipe-leading {
		position: absolute;
		inset: 0 auto 0 0;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 var(--space-4);
		background: color-mix(in srgb, var(--accent) 45%, var(--surface));
		color: var(--text);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.swipe-leading.armed {
		background: var(--accent);
		color: var(--on-accent);
	}
	@media (prefers-reduced-motion: reduce) {
		.swipe-content.animate {
			transition: none;
		}
	}
</style>
