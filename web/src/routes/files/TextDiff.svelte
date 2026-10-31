<script lang="ts">
	import { textDiff } from '#lib/text-diff.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	let { before, after }: { before: string; after: string } = $props();
	const result = $derived(textDiff(before, after));
	let left = $state<HTMLDivElement>(),
		right = $state<HTMLDivElement>();
	let top = $state(0);
	const start = $derived(Math.max(0, Math.floor(top / 28) - 8));
	const shown = $derived(result.rows.slice(start, start + 40));
	function sync(source: HTMLDivElement | undefined, target: HTMLDivElement | undefined) {
		if (!source || !target) return;
		top = source.scrollTop;
		if (target.scrollTop !== source.scrollTop) target.scrollTop = source.scrollTop;
		if (target.scrollLeft !== source.scrollLeft) target.scrollLeft = source.scrollLeft;
	}
	// Equal widths ensure that horizontal scrolling stays synchronized too.
	const contentWidth = $derived(
		result.rows.reduce(
			(max, row) => Math.max(max, row.before?.length ?? 0, row.after?.length ?? 0),
			40,
		) + 10,
	);
</script>

<p class="legend">{t('files.diffLegend')}</p>
{#if result.approximate}<p class="notice-warn">{t('files.diffApproximate')}</p>{/if}
<div class="versions">
	{#each ['before', 'after'] as side}
		<section>
			<h4>{t(side === 'before' ? 'files.previousVersion' : 'files.currentVersion')}</h4>
			{#snippet lines()}
				<div
					class="lines mono"
					style:height={`${result.rows.length * 28}px`}
					style:width={`${contentWidth}ch`}
				>
					{#each shown as row, index (start + index)}
						<div class="line" data-kind={row.kind} style:top={`${(start + index) * 28}px`}>
							<span class="number"
								>{side === 'before' ? (row.beforeNumber ?? '') : (row.afterNumber ?? '')}</span
							>
							<span class="marker" aria-label={t(`files.diff.${row.kind}`)}
								>{row.kind === 'same'
									? ' '
									: row.kind === 'changed'
										? '~'
										: row.kind === 'added'
											? '+'
											: '−'}</span
							>
							<span>{(side === 'before' ? row.before : row.after) ?? ''}</span>
						</div>
					{/each}
				</div>
			{/snippet}
			{#if side === 'before'}
				<!-- Keyboard scroll access for read-only comparison. -->
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					class="scroll"
					bind:this={left}
					onscroll={() => sync(left, right)}
					tabindex="0"
					role="region"
					aria-label={t('files.previousVersion')}
				>
					{@render lines()}
				</div>
			{:else}
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<div
					class="scroll"
					bind:this={right}
					onscroll={() => sync(right, left)}
					tabindex="0"
					role="region"
					aria-label={t('files.currentVersion')}
				>
					{@render lines()}
				</div>{/if}
		</section>
	{/each}
</div>

<style>
	.versions {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	.versions section {
		min-width: 0;
	}
	h4 {
		margin: 8px 0;
	}
	.scroll {
		height: 280px;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
	}
	.lines {
		position: relative;
		min-width: 100%;
		font-size: var(--text-base);
	}
	.line {
		position: absolute;
		height: 28px;
		line-height: 28px;
		left: 0;
		right: 0;
		white-space: pre;
		display: flex;
		background: var(--bg);
	}
	.number {
		box-sizing: content-box;
		position: sticky;
		left: 0;
		z-index: 1;
		background: inherit;
		width: 6ch;
		text-align: right;
		flex: none;
		color: var(--muted);
		padding-right: 1ch;
	}
	.marker {
		position: sticky;
		left: 7ch;
		z-index: 1;
		background: inherit;
		width: 2ch;
		flex: none;
	}
	.line[data-kind='added'] {
		background: color-mix(in srgb, var(--ok-text) 15%, var(--bg));
	}
	.line[data-kind='removed'] {
		background: color-mix(in srgb, var(--error-text) 15%, var(--bg));
	}
	.line[data-kind='changed'] {
		background: color-mix(in srgb, var(--accent) 15%, var(--bg));
	}
	.legend {
		font-size: var(--text-base);
		color: var(--muted);
	}
	@media (max-width: 760px) {
		.versions {
			grid-template-columns: 1fr;
		}
	}
</style>
