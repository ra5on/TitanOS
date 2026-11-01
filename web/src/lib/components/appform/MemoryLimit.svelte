<script lang="ts">
	// The memory limit of a container as a list of common sizes; any other
	// value Compose takes ("1536m") stays selectable as it is.
	import { MEMORY_LIMITS, memoryBytes } from '#lib/appform.ts';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	type Props = { value: string; id?: string; onchange?: () => void };
	let { value = $bindable(), id, onchange }: Props = $props();
	const options = $derived(
		value.trim() && !MEMORY_LIMITS.includes(value.trim())
			? [...MEMORY_LIMITS, value.trim()].sort((a, b) => memoryBytes(a) - memoryBytes(b))
			: MEMORY_LIMITS,
	);
	// Compose counts in powers of 1024 ("4g" = 4 GiB); the list shows its own numbers.
	const label = (limit: string) => {
		const match = /^(\d+(?:\.\d+)?)([kmg])$/i.exec(limit);
		if (match)
			return `${i18n.locale === 'de' ? match[1].replace('.', ',') : match[1]} ${{ k: 'KB', m: 'MB', g: 'GB' }[match[2].toLowerCase()]}`;
		const bytes = memoryBytes(limit);
		return Number.isNaN(bytes) ? limit : formatBytes(bytes);
	};
</script>

<div class="line">
	<select {id} class="field short" bind:value onchange={() => onchange?.()}>
		<option value="">{t('appform.noLimit')}</option>
		{#each options as limit (limit)}<option value={limit}>{label(limit)}</option>{/each}
	</select>
</div>
