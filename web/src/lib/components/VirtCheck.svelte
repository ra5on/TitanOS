<script lang="ts">
	// VT-x/AMD-V and the IOMMU, with what to switch on in the BIOS/UEFI.
	import { virtAdvice, type Virtualization } from '#lib/hwcheck.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Props = {
		virt: Virtualization;
		/** Only problems (VM form); the report shows the good news too. */
		problemsOnly?: boolean;
	};
	let { virt, problemsOnly = false }: Props = $props();

	const advice = $derived(
		virtAdvice(virt).filter((entry) => !problemsOnly || entry.level !== 'ok'),
	);
</script>

{#each advice as entry (entry.key)}
	{#if entry.level === 'warn'}
		<p class="notice-warn" role="status">{t(entry.key as MessageKey)}</p>
	{:else if entry.level === 'ok'}
		<p class="notice-ok">{t(entry.key as MessageKey)}</p>
	{:else}
		<p class="muted note">{t(entry.key as MessageKey)}</p>
	{/if}
{/each}

<style>
	p {
		margin: 0;
	}
	.note {
		font-size: 0.875rem;
	}
</style>
