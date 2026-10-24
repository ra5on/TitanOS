<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import MetricHistory from '#lib/components/MetricHistory.svelte';
	let { values, capacity = 90 }: { values: readonly (number | null)[]; capacity?: number } =
		$props();
</script>

<MetricHistory {values} {capacity} label={t('dashboard.historyRecent')} />
