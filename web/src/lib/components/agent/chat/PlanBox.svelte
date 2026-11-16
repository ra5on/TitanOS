<script lang="ts">
	// The plan the agent keeps for a task (PLAN 55): its points with a box
	// each, done ones ticked and struck through, and "2 von 3 erledigt".
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { PlanStep } from '#lib/agent.ts';

	let { plan }: { plan: PlanStep[] } = $props();
	const done = $derived(plan.filter((s) => s.done).length);
</script>

<section class="plan" aria-label={t('agent.plan')}>
	<header>
		<strong>{t('agent.plan')}</strong>
		<span class="muted">{t('agent.planProgress', { done, total: plan.length })}</span>
	</header>
	<ol>
		{#each plan as step, n (n)}
			<li class:done={step.done}>
				<span class="box" aria-hidden="true">{step.done ? '✓' : ''}</span>
				<span>{step.text}</span>
				{#if step.done}<span class="sr-only">{t('agent.planDone')}</span>{/if}
			</li>
		{/each}
	</ol>
</section>

<style>
	.plan {
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		padding: var(--space-2-5) var(--space-4);
	}
	.plan header {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2);
		flex-wrap: wrap;
		font-size: var(--text-base);
	}
	.plan ol {
		list-style: none;
		margin: var(--space-1-5) 0 0;
		padding: 0;
		display: grid;
		gap: var(--space-1);
	}
	.plan li {
		display: flex;
		gap: var(--space-2);
		align-items: flex-start;
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	.plan li.done > span:nth-child(2) {
		color: var(--muted);
		text-decoration: line-through;
	}
	.plan .box {
		flex: none;
		width: 1.1em;
		height: 1.1em;
		margin-top: 0.15em;
		border: 1.5px solid var(--muted);
		border-radius: 4px;
		display: inline-grid;
		place-items: center;
		font-size: 0.75em;
		line-height: 1;
	}
	.plan li.done .box {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
</style>
