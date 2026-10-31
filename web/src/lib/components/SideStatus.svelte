<script lang="ts">
	// What the status bar shows in "top" mode, at the bottom of the sidebar:
	// the state of the volumes with a dot and words, running rebuilds and
	// checks, the badge for running tasks and "TitanOS x.y · Slot A".
	// Folded only the dot; the words appear beside it on hover and focus.
	import { formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import TasksPanel from './TasksPanel.svelte';

	let {
		folded = false,
		tasks = true,
		note = '',
	}: { folded?: boolean; tasks?: boolean; note?: string } = $props();

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	const state = $derived.by((): { tone: '' | 'ok' | 'warn' | 'error'; lines: string[] } => {
		if (!system.reachable) return { tone: 'error', lines: [t('status.unreachable')] };
		const volumes = storage.status?.volumes ?? [];
		if (volumes.length === 0) return { tone: '', lines: [t('status.noVolumes')] };
		const special = volumes.filter(
			(volume) => volume.state !== 'ok' || volume.sync || volume.encryption?.locked,
		);
		if (special.length === 0)
			return {
				tone: 'ok',
				lines: [
					volumes.length === 1
						? t('status.allOkOne')
						: t('status.allOk', { count: volumes.length }),
				],
			};
		let tone: 'ok' | 'warn' | 'error' = 'ok';
		for (const volume of special) {
			if (volume.state === 'failed' || volume.state === 'missing') tone = 'error';
			else if (
				tone !== 'error' &&
				(volume.state === 'degraded' || volume.state === 'rebuilding' || volume.encryption?.locked)
			)
				tone = 'warn';
		}
		const lines = special.map((volume) => {
			const state = volume.encryption?.locked ? t('crypt.chipLocked') : t(`state.${volume.state}`);
			return volume.sync
				? t('status.volumeSync', {
						name: volume.name,
						state,
						percent: formatPercent(volume.sync.percent),
					})
				: t('status.volume', { name: volume.name, state });
		});
		return { tone, lines };
	});

	const version = $derived.by(() => {
		if (!system.info) return '';
		const parts = [`TitanOS ${system.info.version}`];
		if (system.info.image?.slot)
			parts.push(t('status.slot', { slot: system.info.image.slot.toUpperCase() }));
		return parts.join(' · ');
	});

	const lines = $derived(note ? [note] : state.lines);
	const summary = $derived([...lines, version].filter(Boolean).join(' · '));
</script>

<div class="status" class:folded>
	{#if folded}
		<svelte:element
			this={admin ? 'a' : 'span'}
			class="dot-wrap"
			href={admin ? '/storage' : undefined}
			aria-label={summary}
			><span class="dot {state.tone}" aria-hidden="true"></span><span class="tip">{summary}</span
			></svelte:element
		>
		{#if tasks}<TasksPanel badge />{/if}
	{:else}
		<div class="head">
			<span class="dot {state.tone}" aria-hidden="true"></span>
			<svelte:element
				this={admin ? 'a' : 'span'}
				class="lines"
				href={admin ? '/storage' : undefined}
				title={admin ? t('status.details') : undefined}
			>
				{#each lines as line, i (i)}<span>{line}</span>{/each}
			</svelte:element>
			{#if tasks}<TasksPanel badge />{/if}
		</div>
		{#if version}<span class="version mono">{version}</span>{/if}
	{/if}
</div>

<style>
	.status {
		flex: none;
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 4px 10px 8px;
		font-size: var(--text-sm);
		color: var(--text);
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		min-width: 0;
	}
	.head > .dot {
		margin-top: 5px;
	}
	.lines {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		color: var(--text);
		text-decoration: none;
		overflow-wrap: anywhere;
	}
	a.lines:hover {
		text-decoration: underline;
	}
	.head :global(.tasks-badge) {
		margin-top: -4px;
	}
	.version {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: var(--radius-small);
	}

	.folded {
		align-items: center;
		gap: 6px;
		padding: 4px 0 8px;
	}
	.dot-wrap {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 24px;
	}
	.tip {
		position: absolute;
		left: calc(100% + 18px);
		top: 50%;
		transform: translateY(-50%);
		width: max-content;
		max-width: 280px;
		padding: 6px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--surface);
		box-shadow: var(--shadow-menu);
		color: var(--text);
		font-size: var(--text-base);
		pointer-events: none;
		opacity: 0;
		visibility: hidden;
		z-index: 2;
	}
	.dot-wrap:hover .tip,
	.dot-wrap:focus-visible .tip {
		opacity: 1;
		visibility: visible;
	}
</style>
