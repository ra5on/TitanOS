<script lang="ts">
	// A colour for the agent's working line (owner 16 Nov 2026: "die Farbe
	// für die Punkte und den Text aussuchen"): "Automatisch", a few ready
	// colours and one of one's own. For text it warns when the colour is
	// hard to read on the background (WEB-UI: contrast at least 4.5:1).
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		value = $bindable(''),
		label,
		autoKey,
		text = false,
	}: {
		/** "#rrggbb", or empty for the automatic colour. */
		value?: string;
		label: string;
		autoKey: MessageKey;
		/** A text colour: warn when its contrast is too low. */
		text?: boolean;
	} = $props();

	const COLORS: { key: string; hex: string }[] = [
		{ key: 'orange', hex: '#d9734a' },
		{ key: 'red', hex: '#e5484d' },
		{ key: 'pink', hex: '#e93d82' },
		{ key: 'violet', hex: '#8e4ec6' },
		{ key: 'blue', hex: '#3e63dd' },
		{ key: 'teal', hex: '#12a594' },
		{ key: 'green', hex: '#30a46c' },
		{ key: 'yellow', hex: '#e2a336' },
	];

	let box = $state<HTMLElement>();
	const own = $derived(!!value && !COLORS.some((c) => c.hex === value));

	// Contrast against the page behind the line, as WCAG counts it.
	function lum(rgb: number[]): number {
		const [r, g, b] = rgb.map((v) => {
			const c = v / 255;
			return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
		});
		return 0.2126 * r + 0.7152 * g + 0.0722 * b;
	}
	function background(el: HTMLElement | undefined): number[] | null {
		for (let n: HTMLElement | null = el ?? null; n; n = n.parentElement) {
			const m = getComputedStyle(n).backgroundColor.match(/[\d.]+/g);
			if (m && m.length >= 3 && (m.length < 4 || Number(m[3]) > 0.5))
				return m.slice(0, 3).map(Number);
		}
		return null;
	}
	const weak = $derived.by(() => {
		if (!text || !value || !box) return false;
		const bg = background(box);
		if (!bg) return false;
		const fg = [1, 3, 5].map((i) => parseInt(value.slice(i, i + 2), 16));
		const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
		return (a + 0.05) / (b + 0.05) < 4.5;
	});
</script>

<div class="pick" bind:this={box}>
	<span class="swatches" role="radiogroup" aria-label={label}>
		<label class="sw auto" class:on={!value}>
			<input type="radio" bind:group={value} value="" />
			<span>{t(autoKey)}</span>
		</label>
		{#each COLORS as c (c.key)}
			<label class="sw" class:on={value === c.hex} title={t(`agent.color.${c.key}` as MessageKey)}>
				<input
					type="radio"
					bind:group={value}
					value={c.hex}
					aria-label={t(`agent.color.${c.key}` as MessageKey)}
				/>
				<i style:background={c.hex}></i>
			</label>
		{/each}
		<label class="sw custom" class:on={own} title={t('agent.set.colorCustom')}>
			<input
				type="color"
				value={value || '#d9734a'}
				aria-label={t('agent.set.colorCustom')}
				oninput={(e) => (value = (e.currentTarget as HTMLInputElement).value.toLowerCase())}
			/>
			<span>{t('agent.set.colorCustom')}</span>
		</label>
	</span>
	{#if weak}<small class="weak" role="status">{t('agent.set.colorLowContrast')}</small>{/if}
</div>

<style>
	.pick {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.swatches {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		align-items: center;
	}
	.sw {
		position: relative;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 44px;
		min-height: 44px;
		border-radius: 999px;
		border: 2px solid transparent;
		cursor: pointer;
	}
	.sw input[type='radio'] {
		position: absolute;
		opacity: 0;
		inset: 0;
		margin: 0;
		cursor: pointer;
	}
	.sw i {
		width: 26px;
		height: 26px;
		border-radius: 50%;
		box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--text) 18%, transparent);
	}
	.sw.on {
		border-color: var(--text);
	}
	.sw:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.sw.auto,
	.sw.custom {
		padding: 0 var(--space-3);
		gap: var(--space-1-5);
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--text);
		font-size: var(--text-sm);
	}
	.sw.custom input[type='color'] {
		width: 26px;
		height: 26px;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: none;
		cursor: pointer;
	}
	.weak {
		color: var(--text);
	}
</style>
