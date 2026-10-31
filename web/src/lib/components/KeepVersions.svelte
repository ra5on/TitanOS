<script lang="ts" module>
	/** How many versions stay (0.41.0); since 0.42.0 the UI sets only last or rolling. */
	export type Keep = {
		/** The newest that many stay (0.42.0). */
		last?: number;
		daily: number;
		weekly: number;
		monthly: number;
		mode?: '' | 'rolling';
		dailyAt?: string;
		weeklyAt?: string;
		weeklyDay?: number;
		monthlyAt?: string;
		monthlyDay?: number;
		monthlyNth?: number;
		monthlyWeekday?: number;
		whenFull?: '' | 'oldest';
		maxAgeMonths?: number;
	};

	/** The count a job shows: last, or what its older rules added up to. */
	export function lastOf(k: Keep | null | undefined, fallback = 10): number {
		if (!k) return fallback;
		return Number(k.last) || Number(k.daily) + Number(k.weekly) + Number(k.monthly) || fallback;
	}

	/** What the backend gets: the newest N, or every version until full. */
	export function keepPayload(k: Keep): Keep {
		return k.mode === 'rolling'
			? { daily: 0, weekly: 0, monthly: 0, mode: 'rolling' }
			: { last: lastOf(k), daily: 0, weekly: 0, monthly: 0 };
	}
</script>

<script lang="ts">
	// Settings › Backups and snapshots: which versions stay (decided
	// 31.10.2026): one number, "the newest N", and where the versions do not
	// fill the volume of this NAS also "until the storage is full".
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		keep = $bindable(),
		target = 'storage',
		unit = 'backups',
		rolling = true,
		min = 1,
		max = 9999,
	}: {
		keep: Keep;
		/** What "full" refers to. */
		target?: 'drive' | 'volume' | 'share' | 'storage' | 'replica';
		/** What is counted. */
		unit?: 'backups' | 'snapshots';
		/** Offer "until full"; off where versions share the volume with the data. */
		rolling?: boolean;
		min?: number;
		max?: number;
	} = $props();

	const name = $props.id();
	const isRolling = $derived(rolling && keep.mode === 'rolling');
	// Always a new object, so that a getter/setter binding sees each change.
	const setLast = (value: number) =>
		(keep = { ...keep, last: Math.min(max, Math.max(min, Math.round(value) || min)) });
	const setMode = (mode: '' | 'rolling') => (keep = { ...keep, mode });
</script>

{#snippet count()}
	<label class="line"
		>{#if rolling}<input
				type="radio"
				{name}
				checked={!isRolling}
				onchange={() => setMode('')}
			/>{/if}<span>{t('keep.lastBefore')}</span>
		<input
			class="field mono count"
			type="number"
			{min}
			{max}
			value={lastOf(keep)}
			disabled={isRolling}
			onchange={(e) => setLast(Number(e.currentTarget.value))}
		/>
		<span>{t(`keep.lastAfter.${unit}` as MessageKey)}</span></label
	>
	<span class="muted" class:indent={rolling}>{t(`keep.lastHint.${unit}` as MessageKey)}</span>
{/snippet}

{#if rolling}
	<div class="modes" role="radiogroup" aria-label={t('backup.keeps')}>
		<div class="mode" class:on={!isRolling}>{@render count()}</div>
		<div class="mode" class:on={isRolling}>
			<label class="line"
				><input type="radio" {name} checked={isRolling} onchange={() => setMode('rolling')} /><span
					>{t(`keep.rolling.${target}` as MessageKey)}</span
				></label
			>
			<span class="muted indent"
				>{t(`keep.rollingHint.${target === 'replica' ? 'replica' : unit}` as MessageKey)}</span
			>
		</div>
	</div>
{:else}
	<div class="single">{@render count()}</div>
{/if}

<style>
	.modes,
	.single {
		display: grid;
		gap: 10px;
	}
	.single {
		gap: 6px;
	}
	.mode {
		display: grid;
		gap: 6px;
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 12px 14px;
	}
	.mode.on {
		border-color: var(--accent);
	}
	.line {
		display: flex;
		gap: 8px;
		align-items: center;
		justify-content: flex-start;
		flex-wrap: wrap;
	}
	.count {
		width: 5rem;
		text-align: center;
	}
	.indent {
		margin-left: 24px;
	}
</style>
