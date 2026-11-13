<script lang="ts" module>
	/** Where a swipe for "Zurück" must start: this close to the left edge (px). */
	export const EDGE = 20;
	/** How far the finger goes to the right for "Zurück", and down for "aktualisieren". */
	export const BACK_DISTANCE = 70;
	export const PULL_DISTANCE = 72;

	/**
	 * iOS Safari (not as app on the home screen) has its own swipe from the
	 * left edge for back; doing it here too would go back twice.
	 */
	export function systemBackSwipe(): boolean {
		if (typeof navigator === 'undefined') return false;
		const ios =
			/iPad|iPhone|iPod/.test(navigator.userAgent) ||
			(navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
		const standalone =
			matchMedia('(display-mode: standalone)').matches ||
			(navigator as Navigator & { standalone?: boolean }).standalone === true;
		return ios && !standalone;
	}
</script>

<script lang="ts">
	// PLAN 87: gestures of a program on the phone. A swipe from the left edge
	// goes back – within the program's pages, then to the home screen; a
	// small zone only, and not where the system has one of its own (iOS
	// Safari) or a page needs the touch (data-no-edge-swipe: VM screen …).
	// Pulling the page down at its top refreshes it: the page's own command
	// "Aktualisieren"/"Neu laden" of its menu. The browser's own
	// pull-to-refresh (it would load the whole UI anew) is off on the phone.
	import { goto, afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { localCommandStates, runLocalCommand } from '../menu-commands.svelte.ts';

	// Steps taken inside TitanOS since it loaded: "Zurück" goes back while there are some.
	let depth = 0;
	afterNavigate(({ type }) => {
		if (type === 'popstate') depth = Math.max(0, depth - 1);
		else if (type === 'link' || type === 'goto') depth++;
	});

	function back() {
		if (depth > 0) history.back();
		else void goto('/');
	}

	/** The page's own refresh command, if it has one. */
	function refreshCommand(): string | null {
		const states = localCommandStates();
		return (
			Object.keys(states).find(
				(id) => /\.(refresh|reload)$/.test(id) && states[id]?.enabled !== false,
			) ?? null
		);
	}

	let edge = $state(0);
	let pull = $state(0);
	let refreshing = $state(false);
	let track: {
		x: number;
		y: number;
		kind: 'edge' | 'pull' | null;
		decided: boolean;
	} | null = null;

	function home() {
		return page.url.pathname === '/';
	}
	function blocked(target: EventTarget | null) {
		const el = target as HTMLElement | null;
		return !!el?.closest?.('[data-no-edge-swipe], dialog[open], [popover]:popover-open');
	}

	function start(event: TouchEvent) {
		if (home() || event.touches.length !== 1 || blocked(event.target)) {
			track = null;
			return;
		}
		const touch = event.touches[0];
		const atEdge = touch.clientX <= EDGE && !systemBackSwipe();
		const atTop =
			window.scrollY <= 0 &&
			!(event.target as HTMLElement).closest?.('header.phone-bar') &&
			!scrolledInside(event.target);
		track = {
			x: touch.clientX,
			y: touch.clientY,
			kind: atEdge ? 'edge' : atTop ? 'pull' : null,
			decided: false,
		};
		if (!track.kind) track = null;
	}
	/** A list scrolled inside its own box is not at its top. */
	function scrolledInside(target: EventTarget | null) {
		let el = target as HTMLElement | null;
		while (el && el !== document.body) {
			if (el.scrollTop > 0 && el.scrollHeight > el.clientHeight + 1) return true;
			el = el.parentElement;
		}
		return false;
	}
	function move(event: TouchEvent) {
		if (!track) return;
		const touch = event.touches[0];
		const dx = touch.clientX - track.x;
		const dy = touch.clientY - track.y;
		if (!track.decided) {
			if (Math.hypot(dx, dy) < 10) return;
			track.decided = true;
			const sideways = Math.abs(dx) > Math.abs(dy) * 1.3;
			if (track.kind === 'edge' && !(sideways && dx > 0)) track.kind = null;
			if (track.kind === 'pull' && !(dy > 0 && !sideways && refreshCommand())) track.kind = null;
			if (!track.kind) {
				track = null;
				return;
			}
		}
		if (event.cancelable) event.preventDefault();
		if (track.kind === 'edge') edge = Math.max(0, dx);
		else pull = Math.max(0, dy);
	}
	function end() {
		if (!track) return;
		const kind = track.kind;
		track = null;
		const doBack = kind === 'edge' && edge > BACK_DISTANCE;
		const doRefresh = kind === 'pull' && pull > PULL_DISTANCE;
		edge = 0;
		pull = 0;
		if (doBack) back();
		if (doRefresh) {
			const id = refreshCommand();
			if (id) {
				refreshing = true;
				runLocalCommand(id);
				setTimeout(() => (refreshing = false), 900);
			}
		}
	}

	$effect(() => {
		// The browser's own pull-to-refresh would load all of TitanOS anew.
		const root = document.documentElement.style;
		root.setProperty('overscroll-behavior-y', 'contain');
		const opts = { passive: false } as const;
		document.addEventListener('touchstart', start, { passive: true });
		document.addEventListener('touchmove', move, opts);
		document.addEventListener('touchend', end);
		document.addEventListener('touchcancel', end);
		return () => {
			root.removeProperty('overscroll-behavior-y');
			document.removeEventListener('touchstart', start);
			document.removeEventListener('touchmove', move);
			document.removeEventListener('touchend', end);
			document.removeEventListener('touchcancel', end);
		};
	});
</script>

{#if edge > 0}
	<div
		class="edge-back glass-panel"
		class:armed={edge > BACK_DISTANCE}
		style:--x="{Math.min(edge, 110)}px"
		data-edge-back
		aria-hidden="true"
	>
		<Icon name="chevronLeft" size={22} />
	</div>
{/if}
{#if pull > 0 || refreshing}
	<div
		class="pull-refresh glass-panel"
		class:armed={pull > PULL_DISTANCE || refreshing}
		style:--y="{refreshing ? PULL_DISTANCE : Math.min(pull, 110)}px"
		data-pull-refresh
		role="status"
	>
		<span class="spin" class:on={refreshing}><Icon name="refresh" size={18} /></span>
		<span
			>{t(
				refreshing
					? 'phone.refresh.running'
					: pull > PULL_DISTANCE
						? 'phone.refresh.release'
						: 'phone.refresh.pull',
			)}</span
		>
	</div>
{/if}

<style>
	.edge-back {
		position: fixed;
		z-index: 80;
		left: 0;
		top: 50%;
		width: 48px;
		height: 48px;
		display: grid;
		place-items: center;
		border-radius: 50%;
		color: var(--text);
		transform: translate(calc(var(--x) * 0.6 - 48px), -50%);
		pointer-events: none;
	}
	.edge-back.armed {
		background: var(--accent);
		color: var(--on-accent);
	}
	.pull-refresh {
		position: fixed;
		z-index: 80;
		left: 50%;
		top: calc(var(--header-height, 52px) + 4px);
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-radius: 999px;
		color: var(--text);
		font-size: var(--text-md);
		transform: translate(-50%, calc(var(--y) * 0.5 - 20px));
		pointer-events: none;
	}
	.pull-refresh.armed {
		border-color: var(--accent);
	}
	.spin {
		display: inline-flex;
	}
	.spin.on {
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spin.on {
			animation: none;
		}
	}
</style>
