<script lang="ts" module>
	/** What the backend gets: the count, and rules only for "until full". */
	export function keepChoicePayload(
		count: number,
		versions: Keep | null | undefined,
		min: number,
		max: number,
	): { keep: number; versions: Keep | undefined } {
		if (versions?.mode === 'rolling') return { keep: max, versions: keepPayload(versions) };
		return { keep: Math.min(max, Math.max(min, Number(count) || min)), versions: undefined };
	}
</script>

<script lang="ts">
	// How many versions stay, for snapshots, replication and the stick
	// backup (decided 31.10.2026): one number like data backups; "until
	// full" only for replication, whose versions lie on the other NAS. Rules
	// by day, week and month from 0.41.0 show as their total and go on save.
	import KeepVersions, { keepPayload, type Keep } from './KeepVersions.svelte';

	let {
		count = $bindable(),
		versions = $bindable(),
		min = 1,
		max = 100,
		unit,
		rolling = false,
	}: {
		count: number;
		versions: Keep | null | undefined;
		min?: number;
		max?: number;
		/** What is counted. */
		unit: 'backups' | 'snapshots';
		/** Offer "until the volume of the other NAS runs short". */
		rolling?: boolean;
	} = $props();
</script>

<KeepVersions
	bind:keep={
		() => ({
			last: count,
			daily: 0,
			weekly: 0,
			monthly: 0,
			mode: versions?.mode === 'rolling' ? 'rolling' : '',
		}),
		(k) => {
			count = k.last ?? count;
			versions = k.mode === 'rolling' ? keepPayload(k) : null;
		}
	}
	{unit}
	{rolling}
	{min}
	{max}
	target="replica"
/>
