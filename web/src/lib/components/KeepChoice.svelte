<script lang="ts" module>
	/** What the backend gets: the rules, and a count for older versions. */
	export function keepChoicePayload(
		count: number,
		versions: Keep | null | undefined,
		min: number,
		max: number,
	): { keep: number; versions: Keep | undefined } {
		if (!versions) return { keep: Number(count) || min, versions: undefined };
		const rules = keepPayload(versions);
		const total = rules.daily + rules.weekly + rules.monthly;
		return { keep: Math.min(max, Math.max(min, total || min)), versions: rules };
	}
</script>

<script lang="ts">
	// How many versions stay, for snapshots, replication and the stick
	// backup (decided 31.10.2026): the newest N as before, or by day, week
	// and month like data backups. The count stays what older versions use.
	import KeepVersions, { keepPayload, type Keep } from './KeepVersions.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		count = $bindable(),
		versions = $bindable(),
		min = 1,
		max = 100,
		unit,
	}: {
		count: number;
		versions: Keep | null | undefined;
		min?: number;
		max?: number;
		/** What is counted, e.g. "Snapshots". */
		unit: string;
	} = $props();

	const name = $props.id();
	function byRules(on: boolean) {
		versions = on ? { daily: 7, weekly: 4, monthly: 6, weeklyDay: 0 } : null;
	}
</script>

<div class="choice">
	<label class="check"
		><input type="radio" {name} checked={!versions} onchange={() => byRules(false)} /><span
			>{t('keep.newestN')}</span
		>
		<input
			class="field mono count"
			type="number"
			{min}
			{max}
			bind:value={count}
			disabled={!!versions}
			aria-label={unit}
		/>
		<span>{unit}</span></label
	>
	<label class="check"
		><input type="radio" {name} checked={!!versions} onchange={() => byRules(true)} /><span
			>{t('keep.byDayWeekMonth')}</span
		></label
	>
	{#if versions}
		<KeepVersions bind:keep={versions} times={[]} simple />
	{/if}
</div>

<style>
	.choice {
		display: grid;
		gap: 8px;
	}
	.check {
		display: flex;
		gap: 8px;
		align-items: center;
		flex-wrap: wrap;
	}
	.count {
		width: 5rem;
		text-align: center;
	}
</style>
