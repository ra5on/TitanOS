<script lang="ts">
	import Segments from '#lib/components/Segments.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { PHONE_DOCK_SIZES, type PhoneDockSize } from './phone-home.ts';
	let { size, onchange }: { size: PhoneDockSize; onchange: (size: PhoneDockSize) => void } =
		$props();
	const options = $derived(
		PHONE_DOCK_SIZES.map((value) => ({ value, label: t(`phone.nord.${value}`) })),
	);
</script>

<Segments {options} value={size} label={t('phone.nord.size')} {onchange} />
