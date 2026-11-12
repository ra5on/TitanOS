<script lang="ts">
	// What woke a sleeping disk (PLAN 79-6): the last wakes with their cause,
	// folded away under the state of the disk. Shown only when titand
	// recorded one.
	import type { DiskSleepDisk } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { wakeCausesText, wakeHints, wakeTime } from './sleep-view.ts';

	let { disk }: { disk: DiskSleepDisk } = $props();
	const wakes = $derived(disk.wakes ?? []);
	const hints = $derived(wakeHints(disk));
</script>

{#if wakes.length > 0}
	<details class="wakes">
		<summary
			>{t('storage.wakeHistory', { count: wakes.length })}{#if disk.wakesToday}<span
					class="today muted">{t('storage.wakeCountToday', { count: disk.wakesToday })}</span
				>{/if}</summary
		>
		<ul>
			{#each wakes as wake, index (index)}
				<li>
					<span class="when">{wakeTime(wake.at)}</span>
					<span class="cause">{wakeCausesText(wake)}</span>
				</li>
			{/each}
		</ul>
		{#each hints as hint (hint)}
			<p class="hint muted">
				{#if hint === 'systemData'}{t('storage.wakeHint.systemData')}
					<a href="/settings/systemdata">{t('storage.wakeHint.systemDataLink')}</a>
				{:else if hint === 'likely'}{t('storage.wakeHint.likely')}
				{:else}{t('storage.wakeHint.unknown')}{/if}
			</p>
		{/each}
	</details>
{/if}

<style>
	.wakes {
		font-size: var(--text-sm);
		min-width: 0;
	}
	summary {
		cursor: pointer;
		color: var(--accent);
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--space-2);
		align-items: baseline;
	}
	summary::before {
		content: '›';
		display: inline-block;
		transition: transform 0.12s;
	}
	details[open] > summary::before {
		transform: rotate(90deg);
	}
	summary::-webkit-details-marker {
		display: none;
	}
	ul {
		list-style: none;
		margin: var(--space-1-5) 0 0;
		padding: 0;
		display: grid;
		gap: var(--space-1);
	}
	li {
		display: grid;
		grid-template-columns: minmax(7.5rem, max-content) minmax(0, 1fr);
		gap: 0 var(--space-2-5);
	}
	.when {
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.cause {
		overflow-wrap: anywhere;
	}
	.hint,
	.today {
		font-size: inherit;
		line-height: inherit;
	}
	.hint {
		margin: var(--space-1-5) 0 0;
	}
	@media (max-width: 480px) {
		li {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
