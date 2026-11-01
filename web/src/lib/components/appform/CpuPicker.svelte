<script lang="ts">
	// The CPUs a container may use, one box per thread; none ticked means all.
	// Writes Compose's cpuset ("8-11"). A cpuset the boxes cannot show stays
	// as text.
	import { cpusetCpus, cpusetText } from '#lib/appform.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	type Props = { cpuset: string; threads: number; onchange?: () => void };
	let { cpuset = $bindable(), threads, onchange }: Props = $props();
	const chosen = $derived(cpusetCpus(cpuset));
	const count = $derived(Math.max(threads, ...(chosen ?? [0]).map((cpu) => cpu + 1)));
	function toggle(cpu: number) {
		const now = chosen ?? [];
		cpuset = cpusetText(now.includes(cpu) ? now.filter((c) => c !== cpu) : [...now, cpu]);
		onchange?.();
	}
</script>

{#if chosen === null || count > 256}
	<div class="line">
		<input
			class="field mono"
			type="text"
			bind:value={cpuset}
			oninput={onchange}
			spellcheck="false"
			aria-label={t('appform.cpus')}
		/>
	</div>
{:else}
	<div class="cpus" role="group" aria-label={t('appform.cpus')}>
		{#each Array.from({ length: count }, (_, i) => i) as cpu (cpu)}
			<label class="cpu" class:on={chosen.includes(cpu)}
				><input type="checkbox" checked={chosen.includes(cpu)} onchange={() => toggle(cpu)} /><span
					class="mono">{cpu}</span
				></label
			>
		{/each}
	</div>
{/if}

<style>
	.cpus {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		max-width: 34rem;
		padding-top: 4px;
	}
	.cpu {
		display: inline-flex;
		flex-direction: column;
		align-items: center;
		gap: 3px;
		min-width: 2.75rem;
		padding: 5px 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		color: var(--muted);
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.cpu.on {
		border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
		background: color-mix(in srgb, var(--accent) 10%, transparent);
		color: var(--text);
	}
</style>
