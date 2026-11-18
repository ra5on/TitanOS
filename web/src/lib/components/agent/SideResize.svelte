<script lang="ts" module>
	// 1.07 (owner 17 Nov 2026: "die Seitenleiste … frei in der Breite
	// variieren"): the width of the agent's side bar on the desktop, per
	// device. The default stays the width of before. Since 1.12 the
	// separator itself is ColumnResize (also T-Mail's columns).
	import { clampWidth, readWidth, storeWidth } from '../ColumnResize.svelte';

	export const SIDE_DEFAULT = 264;
	export const SIDE_MIN = 200;
	export const SIDE_MAX = 480;
	const KEY = 'titanos.agent.sideWidth';

	export const clampSide = (w: number) => clampWidth(w, SIDE_MIN, SIDE_MAX, SIDE_DEFAULT);

	/** The width this browser keeps; the default without one. */
	export function readSideWidth(): number {
		return readWidth(KEY, SIDE_MIN, SIDE_MAX, SIDE_DEFAULT);
	}
	export function storeSideWidth(w: number) {
		storeWidth(KEY, w, SIDE_DEFAULT);
	}
</script>

<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import ColumnResize from '../ColumnResize.svelte';

	let { width = $bindable(SIDE_DEFAULT) }: { width?: number } = $props();
</script>

<ColumnResize
	bind:width
	min={SIDE_MIN}
	max={SIDE_MAX}
	fallback={SIDE_DEFAULT}
	key={KEY}
	label={t('agent.sideResize')}
	hint={t('agent.sideResizeHint')}
	left="var(--ag-side-w, 264px)"
/>
