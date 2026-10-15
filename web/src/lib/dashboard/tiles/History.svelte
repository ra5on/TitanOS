<script lang="ts">
 import { t } from '#lib/i18n/index.svelte.ts';
 let { values }: { values: number[] } = $props();
 const path = $derived(values.map((v, i) => `${i ? 'L' : 'M'}${i * 240 / 89},${60 - Math.max(0, Math.min(100, v)) * 0.56}`).join(' '));
</script>
<svg viewBox="0 0 240 64" preserveAspectRatio="none" role="img" aria-label={t('dashboard.historyRecent')}><path class="guide" d="M0 4H240M0 32H240M0 60H240" /><path d={path} /></svg>
<style>
 svg { width: 100%; height: 64px; min-height: 40px; flex: none; fill: none; }
 path { stroke: var(--accent); stroke-width: 2; vector-effect: non-scaling-stroke; }
 .guide { stroke: var(--panel-border); stroke-width: 1; }
</style>
