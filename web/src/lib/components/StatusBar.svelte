<script lang="ts">
	import { formatPercent, formatTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Props = { note?: string };
	/** Replaces the state on the left, e.g. while the dashboard is being edited. */
	let { note = '' }: Props = $props();

	let now = $state(new Date());
	$effect(() => {
		const timer = setInterval(() => (now = new Date()), 10000);
		return () => clearInterval(timer);
	});

	const left = $derived.by(() => {
		if (!system.reachable) return t('status.unreachable');
		const volumes = storage.status?.volumes ?? [];
		if (volumes.length === 0) return t('status.noVolumes');
		return volumes
			.map((volume) => {
				const state = volume.encryption?.locked
					? t('crypt.chipLocked')
					: t(`state.${volume.state}`);
				return volume.sync
					? t('status.volumeSync', {
							name: volume.name,
							state,
							percent: formatPercent(volume.sync.percent),
						})
					: t('status.volume', { name: volume.name, state });
			})
			.join(' · ');
	});

	const right = $derived.by(() => {
		const parts: string[] = [];
		if (system.info) {
			parts.push(`TitanOS ${system.info.version}`);
			if (system.info.image?.slot)
				parts.push(t('status.slot', { slot: system.info.image.slot.toUpperCase() }));
		}
		parts.push(formatTime(now));
		return parts.join(' · ');
	});
</script>

<footer class="mono bar-surface">
	<span>{note || left}</span>
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
		border-top: 1px solid var(--border);
		font-size: 0.75rem;
		color: var(--muted);
	}
	@media (max-width: 760px) {
		footer {
			padding: 10px 16px;
		}
	}
	footer span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
</style>
