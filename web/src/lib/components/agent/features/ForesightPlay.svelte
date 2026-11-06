<script lang="ts">
	// The "Durchspielen" button of a disk or volume on the storage page
	// (PLAN 54-V). Shown only for owners and administrators while
	// Was-wäre-wenn is on; opens the what-if page for this volume.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { scenariosFor, whatIfOn, type Scenario } from '#lib/features/foresight.ts';
	import ForesightWhatIf from './ForesightWhatIf.svelte';

	type Vol = { name: string; level: string; filesystem: string; members: { disk: string }[] };
	type Props = {
		volume: Vol | undefined;
		/** Start with this disk failing; else with the first member. */
		disk?: string;
	};
	let { volume, disk }: Props = $props();
	let levels = $state<string[] | null>(null);
	let open = $state(false);
	$effect(() => {
		void whatIfOn().then((s) => (levels = s.enabled ? s.levels : null));
	});
	const scenarios = $derived(volume && levels ? scenariosFor(volume, levels) : []);
	const start = $derived<Scenario | undefined>(
		disk && volume
			? { kind: 'disk', disk, volume: volume.name }
			: (scenarios[0] ?? (volume ? { kind: 'full', volume: volume.name } : undefined)),
	);
</script>

{#if levels && volume && start}
	<button type="button" class="btn" onclick={() => (open = true)}
		>🧪 {t('agent.f.foresight.play')}</button
	>
	{#if open}
		<ForesightWhatIf {scenarios} {start} onclose={() => (open = false)} />
	{/if}
{/if}
