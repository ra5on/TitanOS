<script lang="ts">
	import { formatTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Props = { note?: string };
	/** Replaces the state on the left, e.g. while the dashboard is being edited. */
	let { note = '' }: Props = $props();

	let now = $state(new Date());
	$effect(() => {
		const timer = setInterval(() => (now = new Date()), 10000);
		return () => clearInterval(timer);
	});

	const right = $derived.by(() => {
		const parts: string[] = [];
		if (system.info) {
			parts.push(`TitanOS ${system.info.version}`);
			if (system.info.image?.slot) parts.push(t('status.slot', { slot: system.info.image.slot.toUpperCase() }));
		}
		parts.push(formatTime(now));
		return parts.join(' · ');
	});
</script>

<footer class="mono">
	<span>{note || (system.reachable ? t('status.noVolumes') : t('status.unreachable'))}</span>
	<span>{right}</span>
</footer>

<style>
	footer {
		position: relative;
		z-index: 1;
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 6px 24px;
		padding: 10px 32px;
		background: var(--header);
		border-top: 1px solid var(--border);
		font-size: 0.75rem;
		color: var(--muted);
	}
	@media (max-width: 760px) {
		footer {
			padding: 10px 16px;
		}
	}
</style>
