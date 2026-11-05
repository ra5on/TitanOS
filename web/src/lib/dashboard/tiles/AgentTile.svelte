<script lang="ts">
	// The KI-Agent on the dashboard (PLAN 44): tasks that run or wait for
	// the person, the latest chats and a field that opens the agent with the
	// task. It reads the list itself while the dashboard is open.
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo } from '#lib/agent.ts';

	let chats = $state<ChatInfo[] | null>(null);
	let error = $state(false);
	let text = $state('');

	async function load() {
		try {
			chats = (await api.get<{ chats: ChatInfo[] }>('/agent/chats')).chats;
			error = false;
		} catch {
			error = true;
		}
	}
	$effect(() => {
		void load();
		const timer = setInterval(load, 30000);
		return () => clearInterval(timer);
	});
	const active = $derived(
		(chats ?? []).filter((c) => c.state === 'running' || c.state === 'waiting'),
	);
	const recent = $derived(
		(chats ?? [])
			.filter((c) => c.state !== 'running' && c.state !== 'waiting' && c.title)
			.slice(0, 4),
	);
	function ask(event: SubmitEvent) {
		event.preventDefault();
		const value = text.trim();
		void goto('/agent' + (value ? '?' + new URLSearchParams({ ask: value }).toString() : ''));
	}
</script>

<div class="agent-tile">
	<form onsubmit={ask}>
		<input
			class="field"
			bind:value={text}
			placeholder={t('agent.tileAsk')}
			aria-label={t('agent.tileAsk')}
		/>
	</form>
	{#if error}
		<p class="muted">{t('agent.tileUnavailable')}</p>
	{:else if chats}
		{#each active as c (c.id)}
			<a class="row" href={`/agent?chat=${c.id}`}>
				<span class="dot {c.state === 'waiting' ? 'warn' : 'ok'}"></span>
				<span class="name">{c.title || t('agent.newChat')}</span>
				<span class="muted">{c.state === 'waiting' ? t('agent.waiting') : t('agent.running')}</span>
			</a>
		{/each}
		{#each recent as c (c.id)}
			<a class="row" href={`/agent?chat=${c.id}`}>
				<span class="name">{c.title}</span>
			</a>
		{/each}
		{#if !active.length && !recent.length}<p class="muted">{t('agent.noChats')}</p>{/if}
	{/if}
</div>

<style>
	.agent-tile {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-height: 0;
		overflow: hidden;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		color: var(--text);
		text-decoration: none;
		font-size: var(--text-base);
		min-width: 0;
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.row .muted {
		font-size: var(--text-xs);
	}
</style>
