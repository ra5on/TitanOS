<script lang="ts">
	// PLAN 79-13: what a tool or the NAS said in its own words (English,
	// technical) is never the headline. The page says in its language what
	// happened; the original sentence waits behind "Einzelheit". Only
	// phrasing content, so it can stand inside a paragraph, a cell or a line.
	import { t } from '#lib/i18n/index.svelte.ts';

	let { text }: { text: string | null | undefined } = $props();
	let open = $state(false);
	const uid = $props.id();
</script>

{#if text}<span class="bdetail"
		><button
			type="button"
			class="bdetail-toggle"
			aria-expanded={open}
			aria-controls="{uid}-text"
			onclick={(event) => {
				event.stopPropagation();
				open = !open;
			}}>{t('detail.label')}</button
		>{#if open}<span class="bdetail-text" id="{uid}-text">{text}</span>{/if}</span
	>{/if}

<style>
	.bdetail {
		display: block;
		margin-top: var(--space-1);
		font-weight: 400;
	}
	.bdetail-toggle {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		font-size: 0.8125rem;
		text-decoration: underline;
		text-underline-offset: 2px;
		cursor: pointer;
		opacity: 0.85;
	}
	.bdetail-toggle::after {
		content: ' ▾';
	}
	.bdetail-toggle[aria-expanded='true']::after {
		content: ' ▴';
	}
	.bdetail-text {
		display: block;
		margin-top: var(--space-1);
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		user-select: text;
	}
</style>
