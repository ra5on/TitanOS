<script lang="ts">
	import MetricHistory from '#lib/components/MetricHistory.svelte';
	let { values }: { values: number[] } = $props();
</script>

<MetricHistory {values} capacity={30} height={24} adaptive />
