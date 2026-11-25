<script lang="ts">
	import { onMount } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { formatTime } from '#lib/format.ts';
	import type { ChatInfo } from '#lib/agent.ts';
	import BottomSheet from '../BottomSheet.svelte';
	import LightTrail from '../LightTrail.svelte';

	let {
		chats,
		onopen,
		onapproval,
		calm = false,
	}: {
		chats: ChatInfo[];
		onopen: (id: string) => void;
		onapproval: (id: string) => void;
		calm?: boolean;
	} = $props();
	let day = $state(0),
		expanded = $state(false),
		queue = $state(false);
	let today = $state(new Date());
	onMount(() => {
		let timer: ReturnType<typeof setTimeout>;
		function midnight() {
			today = new Date();
			const next = new Date(today);
			next.setHours(24, 0, 0, 0);
			timer = setTimeout(midnight, next.getTime() - today.getTime());
		}
		midnight();
		return () => clearTimeout(timer);
	});
	const own = $derived(
		chats
			.filter((c) => !c.archived && !c.author)
			.sort((a, b) => b.updated.localeCompare(a.updated)),
	);
	const waiting = $derived(own.filter((c) => c.state === 'waiting').reverse());
	const entries = $derived.by(() => {
		const date = new Date(today);
		date.setDate(date.getDate() - day);
		return own.filter((c) => new Date(c.updated).toDateString() === date.toDateString());
	});
	function review(id: string) {
		queue = false;
		onapproval(id);
	}
</script>

{#if waiting.length}
	<button
		class="approval-summary"
		type="button"
		aria-haspopup="dialog"
		onclick={() => (queue = true)}
	>
		<LightTrail />
		<span class="request"
			><small
				><Icon name="shield" size={13} />{waiting.length === 1
					? t('agent.night.waitOne')
					: t('agent.night.wait', { count: waiting.length })}</small
			><strong>{waiting[0].title}</strong></span
		>
		<span class="review">{t('agent.home.look')} <Icon name="chevronRight" size={15} /></span>
	</button>
{/if}
<section class="journal" aria-label={t('agent.night.day')}>
	<header>
		<h2>{t('agent.night.day')}</h2>
		{#if entries.length > 1}<button
				type="button"
				aria-expanded={expanded}
				onclick={() => (expanded = !expanded)}
				>{expanded ? t('agent.home.less') : t('agent.home.all', { count: entries.length })}
				<Icon name={expanded ? 'chevronUp' : 'chevronDown'} size={13} /></button
			>{/if}
		<select
			aria-label={t('agent.night.selectDay')}
			bind:value={day}
			onchange={() => (expanded = false)}
			><option value={0}>{t('agent.night.today')}</option><option value={1}
				>{t('agent.night.yesterday')}</option
			></select
		>
	</header>
	<div class="events" class:expanded>
		{#each expanded ? entries : entries.slice(0, 1) as c (c.id)}
			<button
				type="button"
				class="event"
				onclick={() => (c.state === 'waiting' ? review(c.id) : onopen(c.id))}
			>
				<time datetime={c.updated}>{formatTime(new Date(c.updated))}</time>
				<span class="point" class:pending={c.state === 'waiting'}></span>
				<span class="title">{c.title}</span>
				<Icon name="chevronRight" size={14} />
			</button>
		{:else}<p>{t('agent.night.empty')}</p>{/each}
	</div>
</section>
{#if queue}
	<BottomSheet label={t('agent.night.approvals')} {calm} onclose={() => (queue = false)}>
		<div class="queue-head">
			<h2>{t('agent.night.approvals')}</h2>
			<button class="btn" onclick={() => (queue = false)}>{t('common.close')}</button>
		</div>
		{#each waiting as c (c.id)}<button class="sh-item" type="button" onclick={() => review(c.id)}
				><Icon name="shield" size={18} /><span class="sh-text">{c.title}</span><Icon
					name="chevronRight"
					size={16}
				/></button
			>
		{:else}<p class="sh-group">{t('agent.night.noApprovals')}</p>{/each}
	</BottomSheet>
{/if}

<style>
	.approval-summary,
	.event,
	header button,
	select {
		font: inherit;
		cursor: pointer;
		color: inherit;
	}
	.approval-summary {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		min-height: 52px;
		padding: 6px 12px;
		border: 1px solid color-mix(in srgb, var(--ag-edge) 50%, var(--border));
		border-radius: 14px;
		background: color-mix(in srgb, var(--ag-edge) 10%, var(--ag-glass, var(--surface)));
		text-align: left;
	}
	.request {
		display: grid;
		gap: 1px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.request small,
	.review {
		display: flex;
		align-items: center;
		gap: 5px;
	}
	.request small {
		color: var(--ag-edge);
		font-size: 0.76em;
		font-weight: 600;
	}
	.request strong {
		font-weight: 600;
		line-height: 1.2;
	}
	.review {
		flex: none;
		background: var(--accent-fill, var(--accent));
		color: var(--on-accent);
		font-weight: 600;
		border-radius: 8px;
		padding: 5px 9px;
		font-size: 0.85em;
	}
	.journal {
		min-width: 0;
	}
	header {
		display: flex;
		align-items: center;
		gap: 8px;
		border-bottom: 1px solid var(--border);
		min-height: 42px;
	}
	h2 {
		font-size: 1.06em;
		margin: 0;
		flex: 1;
	}
	header button,
	select {
		appearance: none;
		background: transparent;
		border: 0;
		color: var(--muted);
		min-height: 44px;
		font-size: 0.83em;
	}
	header button {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	select option {
		background: var(--surface);
		color: var(--text);
	}
	.events.expanded {
		max-height: min(34dvh, 19rem);
		overflow: auto;
		overscroll-behavior: contain;
	}
	.event {
		display: flex;
		align-items: center;
		text-align: left;
		gap: 10px;
		min-height: 44px;
		width: 100%;
		background: none;
		border: 0;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 44%, transparent);
		padding: 4px 5px;
	}
	time {
		font-variant-numeric: tabular-nums;
		color: var(--muted);
		font-size: 0.81em;
	}
	.point {
		width: 4px;
		height: 4px;
		background: var(--muted);
		border-radius: 50%;
		flex: none;
	}
	.point.pending {
		background: var(--ag-edge);
	}
	.title {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.event :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.events p {
		font-size: 0.9em;
		color: var(--muted);
		margin: 12px 4px;
	}
	.queue-head {
		padding: 6px 16px;
		display: flex;
		align-items: center;
		gap: 10px;
	}
	@media (hover: hover) {
		.event:hover,
		header button:hover,
		.approval-summary:hover {
			background: color-mix(in srgb, var(--accent) 9%, var(--ag-glass, var(--surface)));
		}
	}
</style>
