<script lang="ts">
	// "Vorschläge" of the agent (PLAN 53): what it learned at night, found
	// out while watching, foresaw or would keep as a skill, as cards with yes
	// or no. Nothing changes before the person decides.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import type { InboxItem } from '#lib/agent.ts';
	import { fly } from 'svelte/transition';

	let {
		calm = false,
		onclose,
		onask,
		oncount,
	}: {
		calm?: boolean;
		onclose: () => void;
		/** Opens a new chat with this question. */
		onask: (text: string) => void;
		oncount?: (n: number) => void;
	} = $props();

	let items = $state<InboxItem[]>([]);
	let error = $state('');
	let filter = $state<'all' | InboxItem['kind']>('all');
	let editing = $state<{ id: string; text: string } | null>(null);
	$effect(() => {
		api
			.get<{ items: InboxItem[] }>('/agent/inbox')
			.then((r) => (items = (r.items ?? []).reverse()))
			.catch((e) => (error = apiErrorText(e)));
	});
	$effect(() => oncount?.(items.length));
	async function decide(item: InboxItem, accept: boolean, text = '') {
		try {
			await api.post(`/agent/inbox/${item.id}`, { accept, text });
			items = items.filter((x) => x.id !== item.id);
			editing = null;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	// 'deadline': Fristenwächter (PLAN 54-D, features/DocsViews.svelte).
	const look: Record<InboxItem['kind'] | 'deadline', { icon: IconName; color: string }> = {
		memory: { icon: 'star', color: '#10b981' },
		conflict: { icon: 'activity', color: '#f59e0b' },
		fade: { icon: 'clock', color: '#64748b' },
		skill: { icon: 'zap', color: '#eab308' },
		forecast: { icon: 'disk', color: '#f97316' },
		alert: { icon: 'bell', color: '#fb7185' },
		deadline: { icon: 'clock', color: '#f59e0b' },
	};
	const kindText = (k: string) =>
		k === 'deadline' ? t('agent.f.docs.inboxKind') : t(`agent.inbox.kind.${k}` as MessageKey);
	const fileHref = (d: Record<string, string>) =>
		`/files?root=${encodeURIComponent(d.root)}&path=${encodeURIComponent(d.path.split('/').slice(0, -1).join('/'))}`;
	const kinds = $derived([...new Set(items.map((i) => i.kind))]);
	const shown = $derived(filter === 'all' ? items : items.filter((i) => i.kind === filter));
	// Yes means remove for old or contradicting memories, keep for the rest.
	const yesLabel = (k: InboxItem['kind']) =>
		k === 'fade' || k === 'conflict'
			? t('agent.inbox.remove')
			: k === 'alert' || k === 'forecast'
				? t('agent.inbox.ask')
				: t('agent.inbox.take');
	const noLabel = (k: InboxItem['kind']) =>
		k === 'fade' || k === 'conflict' ? t('agent.inbox.keep') : t('agent.inbox.no');
	function yes(item: InboxItem) {
		if (item.kind === 'alert' || item.kind === 'forecast') {
			void decide(item, false);
			onask(t('agent.inbox.askText', { title: item.title }));
			return;
		}
		void decide(item, true);
	}
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div class="scrim" role="presentation" onclick={onclose}></div>
<div
	class="panel"
	role="dialog"
	aria-modal="true"
	aria-label={t('agent.inbox.title')}
	transition:fly={{ x: 380, duration: calm ? 0 : 260, opacity: 1 }}
>
	<header>
		<button type="button" class="ib" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={20} /></button
		>
		<h2>
			{t('agent.inbox.title')}<small
				>{items.length ? t('agent.inbox.count', { count: items.length }) : ''}</small
			>
		</h2>
		<span class="ib-space"></span>
	</header>
	<div class="body">
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if kinds.length > 1}
			<div class="filter" role="radiogroup" aria-label={t('agent.inbox.title')}>
				<label class:on={filter === 'all'}
					><input type="radio" bind:group={filter} value="all" />{t('agent.inbox.all')}</label
				>
				{#each kinds as k (k)}
					<label class:on={filter === k}
						><input type="radio" bind:group={filter} value={k} />{kindText(k)}</label
					>
				{/each}
			</div>
		{/if}
		{#each shown as item (item.id)}
			<article class="card" style:--c={(look[item.kind] ?? look.alert).color}>
				<div class="head">
					<span class="sq" aria-hidden="true"
						><Icon name={(look[item.kind] ?? look.alert).icon} size={15} /></span
					>
					<span class="kind">{kindText(item.kind)}</span>
					<time>{formatDateTime(new Date(item.created))}</time>
				</div>
				{#if editing?.id === item.id}
					<textarea class="field" rows="2" maxlength="600" bind:value={editing.text}></textarea>
				{:else}
					<strong>{item.title}</strong>
				{/if}
				{#if item.body}<p>{item.body}</p>{/if}
				<div class="acts">
					<button type="button" class="btn" onclick={() => decide(item, false)}
						>{noLabel(item.kind)}</button
					>
					{#if item.kind === 'memory'}
						{#if editing?.id === item.id}
							<button
								type="button"
								class="btn btn-primary"
								onclick={() => decide(item, true, editing!.text)}>{t('agent.inbox.take')}</button
							>
						{:else}
							<button
								type="button"
								class="btn"
								onclick={() => (editing = { id: item.id, text: item.title })}
								>{t('agent.inbox.edit')}</button
							>
							<button type="button" class="btn btn-primary" onclick={() => yes(item)}
								>{yesLabel(item.kind)}</button
							>
						{/if}
					{:else}
						{#if item.kind === 'deadline' && item.data?.root && item.data?.path}
							<a class="btn" href={fileHref(item.data)}>{t('agent.f.docs.openFile')}</a>
						{/if}
						<button type="button" class="btn btn-primary" onclick={() => yes(item)}
							>{yesLabel(item.kind)}</button
						>
					{/if}
				</div>
			</article>
		{:else}
			<p class="empty">{t('agent.inbox.empty')}</p>
		{/each}
	</div>
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.45);
	}
	.panel {
		position: fixed;
		z-index: 71;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(460px, 100%);
		display: flex;
		flex-direction: column;
		color: var(--text);
		background: var(--bg);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu, -18px 0 44px rgb(0 0 0 / 0.5));
	}
	header {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: calc(env(safe-area-inset-top, 0px) + var(--space-3)) 14px var(--space-2);
	}
	h2 {
		flex: 1;
		margin: 0;
		text-align: center;
		font-size: var(--text-md);
		display: flex;
		flex-direction: column;
	}
	h2 small {
		font-size: var(--text-xs);
		font-weight: 400;
		color: var(--muted);
	}
	.ib,
	.ib-space {
		width: 40px;
		height: 40px;
		flex: none;
	}
	.ib {
		border-radius: 13px;
		display: grid;
		place-items: center;
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		background: color-mix(in srgb, var(--text) 6%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.body {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-1-5) 14px calc(env(safe-area-inset-bottom, 0px) + 18px);
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
	}
	.body > * {
		flex-shrink: 0;
	}
	.filter {
		display: flex;
		gap: var(--space-1-5);
		overflow-x: auto;
		scrollbar-width: none;
	}
	.filter label {
		white-space: nowrap;
		padding: 7px var(--space-3);
		border-radius: 999px;
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--muted);
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		cursor: pointer;
	}
	.filter label.on {
		color: var(--on-accent, #fff);
		background: var(--accent);
		border-color: transparent;
	}
	.filter input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.filter label:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		padding: var(--space-3) 14px;
		border-radius: 20px;
		background: color-mix(in srgb, var(--text) 4%, transparent);
		border: 1px solid color-mix(in srgb, var(--text) 9%, transparent);
	}
	.head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-xs);
		color: var(--muted);
	}
	.head time {
		margin-left: auto;
	}
	.kind {
		font-weight: 600;
	}
	.sq {
		width: 28px;
		height: 28px;
		border-radius: 9px;
		display: grid;
		place-items: center;
		color: var(--muted);
		background: var(--chip);
	}
	.card strong {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.card p {
		margin: 0;
		color: var(--muted);
		font-size: var(--text-sm);
		line-height: 1.45;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}
	.acts .btn-primary {
		margin-left: auto;
	}
	.empty {
		margin: 40px 0;
		text-align: center;
		color: var(--muted);
	}
</style>
