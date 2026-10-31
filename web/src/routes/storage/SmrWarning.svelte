<script lang="ts">
	// Warns before RAID 5/6 on SMR disks. The disk hints come from sysfs
	// only (GET /hardware/disks); without them nothing is shown.
	import { api } from '#lib/api.ts';
	import { smrForLevel, type HwDisk } from '#lib/hwcheck.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';

	type Props = { level: string; selected: string[] };
	let { level, selected }: Props = $props();

	let disks = $state<HwDisk[]>([]);
	onMount(() => {
		api.get<{ disks: HwDisk[] }>('/hardware/disks').then(
			(res) => (disks = res.disks),
			() => {},
		);
	});
	const smr = $derived(smrForLevel(level, selected, disks));
</script>

{#if smr.length > 0}
	<p class="notice-warn" role="alert">
		{t('create.smrWarning', {
			disks: smr.map((disk) => `${disk.name} (${disk.model})`).join(', '),
		})}
	</p>
{/if}
