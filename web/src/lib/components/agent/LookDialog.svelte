<script lang="ts">
	// Darstellung of the KI-Agent (PLAN 44, Bild 10): accent, chat style and
	// effects. Kept in this browser only, so changing it never writes to the
	// stick; light and dark follow the TitanOS theme. "Weniger Bewegung"
	// also follows the system setting.
	import Dialog from '#lib/components/Dialog.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { ACCENTS, type Look } from '#lib/agent.ts';

	let { look = $bindable(), onclose }: { look: Look; onclose: () => void } = $props();
	const effects: {
		key: 'glass' | 'glow' | 'typing' | 'vibrate' | 'calm';
		label: string;
		hint: string;
	}[] = $derived([
		{ key: 'glass', label: t('agent.lookGlass'), hint: t('agent.lookGlassHint') },
		{ key: 'glow', label: t('agent.lookGlow'), hint: t('agent.lookGlowHint') },
		{ key: 'typing', label: t('agent.lookTyping'), hint: t('agent.lookTypingHint') },
		{ key: 'vibrate', label: t('agent.lookVibrate'), hint: t('agent.lookVibrateHint') },
		{ key: 'calm', label: t('agent.lookCalm'), hint: t('agent.lookCalmHint') },
	]);
</script>

<Dialog title={t('agent.look')} {onclose}>
	<p class="muted">{t('agent.lookIntro')}</p>
	<div class="opt">
		<span class="label">{t('agent.lookAccent')}</span>
		<div class="swatches" role="radiogroup" aria-label={t('agent.lookAccent')}>
			{#each Object.entries(ACCENTS) as [key, pair] (key)}
				<button
					type="button"
					role="radio"
					aria-checked={look.accent === key}
					aria-label={t(`agent.accent.${key}` as never)}
					title={t(`agent.accent.${key}` as never)}
					class="sw"
					style:background={pair
						? `linear-gradient(135deg, ${pair[0]}, ${pair[1]})`
						: 'var(--accent)'}
					onclick={() => (look.accent = key as Look['accent'])}
				></button>
			{/each}
		</div>
	</div>
	<div class="opt">
		<span class="label">{t('agent.lookStyle')}</span>
		<Segments
			label={t('agent.lookStyle')}
			bind:value={look.style}
			options={[
				{ value: 'bubbles', label: t('agent.styleBubbles') },
				{ value: 'clean', label: t('agent.styleClean') },
				{ value: 'compact', label: t('agent.styleCompact') },
			]}
		/>
	</div>
	<div class="opt">
		<span class="label">{t('agent.lookBackground')}</span>
		<Segments
			label={t('agent.lookBackground')}
			bind:value={look.background}
			options={[
				{ value: 'aurora', label: t('agent.bgAurora') },
				{ value: 'ocean', label: t('agent.bgOcean') },
				{ value: 'forest', label: t('agent.bgForest') },
				{ value: 'none', label: t('agent.bgNone') },
			]}
		/>
	</div>
	<div class="opt">
		<span class="label">{t('agent.lookEffects')}</span>
		{#each effects as e (e.key)}
			<label class="check effect">
				<input type="checkbox" bind:checked={look[e.key]} />
				<span><strong>{e.label}</strong><small class="muted">{e.hint}</small></span>
			</label>
		{/each}
	</div>
	<div class="end">
		<button type="button" class="btn btn-primary" onclick={onclose}>{t('common.close')}</button>
	</div>
</Dialog>

<style>
	.opt {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-top: var(--space-3);
	}
	.label {
		font-size: var(--text-xs);
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--muted);
	}
	.swatches {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
	}
	.sw {
		width: 34px;
		height: 34px;
		border-radius: 50%;
		border: 0;
		cursor: pointer;
	}
	.sw[aria-checked='true'] {
		outline: 2px solid var(--text);
		outline-offset: 2px;
	}
	.effect {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 6px 0;
		border-bottom: 1px solid var(--border);
	}
	.effect span {
		display: flex;
		flex-direction: column;
	}
	.end {
		display: flex;
		justify-content: flex-end;
		margin-top: var(--space-4);
	}
</style>
