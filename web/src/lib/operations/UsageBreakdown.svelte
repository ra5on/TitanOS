<script lang="ts">
	import { formatBytes } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	let { entries }: { entries: { kind?: string; bytes: number }[] } = $props();
	const groups = $derived.by(() => {
		const totals = new Map<string, number>();
		for (const entry of entries) {
			const key = entry.kind ?? 'other';
			totals.set(key, (totals.get(key) ?? 0) + entry.bytes);
		}
		return [...totals].filter(([, bytes]) => bytes > 0).sort((a, b) => b[1] - a[1]);
	});
	const total = $derived(groups.reduce((sum, [, bytes]) => sum + bytes, 0));
</script>

{#if total > 0}<div class="breakdown">
		<p class="muted">{t('tasks.usageChart')}</p>
		<div class="bar" role="img" aria-label={t('tasks.usageChart')}>
			{#each groups as [kind, bytes], i (kind)}<span
					style:flex-grow={bytes / total}
					style:opacity={1 - i * 0.1}
					title={`${t(`ops.usage.${kind}` as MessageKey)}: ${formatBytes(bytes)}`}
				></span>{/each}
		</div>
		<ul>
			{#each groups as [kind, bytes] (kind)}<li>
					<span>{t(`ops.usage.${kind}` as MessageKey)}</span><strong>{formatBytes(bytes)}</strong>
				</li>{/each}
		</ul>
	</div>{/if}

<style>
	.bar {
		display: flex;
		height: 22px;
		border-radius: var(--radius-ctl);
		overflow: hidden;
		gap: 2px;
		background: var(--border);
	}
	.bar span {
		background: var(--accent);
		min-width: 2px;
	}
	ul {
		display: flex;
		gap: 12px 24px;
		flex-wrap: wrap;
		list-style: none;
		padding: 0;
	}
	li {
		display: flex;
		gap: 10px;
		font-size: var(--text-base);
	}
	strong {
		font-family: var(--font-mono);
		font-weight: 400;
	}
</style>
