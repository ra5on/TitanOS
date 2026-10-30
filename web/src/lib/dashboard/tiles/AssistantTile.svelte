<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { tileOptions, type TileOptions } from '../options.ts';
	let { options = tileOptions(null, 'assistant') }: { options?: TileOptions } = $props();
	// The latest tasks of this account and the next automations. Reads only
	// what the assistant pages read; refreshes while the dashboard is visible.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { ChatSummary } from '#lib/assistant.svelte.ts';

	type Automation = { id: string; name: string; enabled: boolean; next?: string };
	let chats = $state<ChatSummary[] | null>(null);
	let next = $state<Automation[]>([]);
	let unavailable = $state(false);

	async function load() {
		try {
			const res = await api.get<{ chats: ChatSummary[] }>('/assistant/chats');
			chats = res.chats.toSorted((a, b) => b.updated.localeCompare(a.updated)).slice(0, 5);
			unavailable = false;
		} catch {
			unavailable = true;
			return;
		}
		try {
			const res = await api.get<{ automations: Automation[] }>('/assistant/automations');
			next = res.automations
				.filter((a) => a.enabled && a.next && !a.next.startsWith('0001-'))
				.toSorted((a, b) => a.next!.localeCompare(b.next!))
				.slice(0, 3);
		} catch {
			next = [];
		}
	}

	onMount(() => poll(load, { interval: 30000 }));

	const CHIP: Record<string, string> = {
		confirmation: 'warn',
		blocked: 'error',
		running: 'info',
		queued: 'info',
		waiting: 'info',
		done: 'ok',
	};
</script>

{#if unavailable}
	<p class="muted">{t('tile.assistantUnavailable')}</p>
{:else if chats === null}
	<p class="muted">…</p>
{:else}
	{#if chats.length === 0}
		<p class="muted">{t('tile.assistantEmpty')}</p>
	{:else}
		<ul>
			{#each chats as chat (chat.id)}
				<li>
					<a href="/assistant?chat={encodeURIComponent(chat.id)}"
						>{chat.title || t('assistant.untitled')}</a
					>
					{#if chat.run?.state}<span class="chip {CHIP[chat.run.state] ?? ''}"
							>{t(`agent.state.${chat.run.state}` as MessageKey)}</span
						>{/if}
				</li>
			{/each}
		</ul>
	{/if}
	{#if options.details && next.length}
		<h3>{t('tile.assistantNext')}</h3>
		<ul>
			{#each next as a (a.id)}
				<li>
					<a href="/assistant/automations">{a.name}</a><span class="muted mono"
						>{formatDateTime(new Date(a.next!))}</span
					>
				</li>
			{/each}
		</ul>
	{/if}
{/if}

<style>
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	li {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		min-width: 0;
		font-size: 0.875rem;
	}
	li a {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.chip {
		flex: none;
		font-size: 0.6875rem;
	}
	h3 {
		margin: 14px 0 8px;
		font-size: 0.75rem;
		font-weight: 600;
		color: var(--muted);
		text-transform: uppercase;
	}
</style>
