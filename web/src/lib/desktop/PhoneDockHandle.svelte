<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import { desktop } from './wm.svelte.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	let start: { x: number; y: number; id: number } | null = null;
	function down(e: PointerEvent) {
		if (!e.isPrimary || e.button !== 0) return;
		start = { x: e.clientX, y: e.clientY, id: e.pointerId };
		e.currentTarget instanceof HTMLElement && e.currentTarget.setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!start || start.id !== e.pointerId) return;
		const dx = Math.abs(e.clientX - start.x),
			up = start.y - e.clientY;
		if (dx > 12 && dx > up) start = null;
		else if (up > 18 && up > dx) {
			start = null;
			phoneChrome.dock = true;
		}
	}
</script>

{#if desktop.prefs.phone.dock.show}
	<button
		type="button"
		class="dock-handle"
		data-no-edge-swipe
		data-phone-dock-handle
		aria-label={t('phone.nord.showDock')}
		title={t('phone.nord.showDock')}
		aria-haspopup="dialog"
		onpointerdown={down}
		onpointermove={move}
		onpointerup={() => (start = null)}
		onpointercancel={() => (start = null)}
		onclick={() => (phoneChrome.dock = true)}
	>
		<span aria-hidden="true"></span>
	</button>
{/if}

<style>
	.dock-handle {
		display: grid;
		flex: none;
		place-items: center;
		width: 48px;
		height: 44px;
		border: 0;
		border-radius: 16px;
		background: transparent;
		color: inherit;
		cursor: pointer;
		touch-action: none;
	}
	.dock-handle span {
		width: 24px;
		height: 4px;
		background: currentColor;
		opacity: 0.8;
		border-radius: 9px;
	}
</style>
