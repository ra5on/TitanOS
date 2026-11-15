<script lang="ts">
	// The projects as a list of their own on the phone (the tab "Projekte",
	// 15 Nov 2026): one row each with its sign, its name and how many chats
	// it holds; a tap opens the project, the button below starts a new one.
	import Icon from '#lib/components/Icon.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo, Project } from '#lib/agent.ts';

	let {
		projects,
		chats,
		onopen,
		onnew,
	}: {
		projects: Project[];
		chats: ChatInfo[];
		onopen: (p: Project) => void;
		onnew: () => void;
	} = $props();

	function sub(p: Project): string {
		const mine = chats.filter((c) => c.project === p.id && !c.archived);
		const last = mine
			.map((c) => c.updated)
			.sort()
			.at(-1);
		const count =
			mine.length === 1 ? t('agent.tabs.chatOne') : t('agent.tabs.chats', { count: mine.length });
		return last ? `${count} · ${formatDateTime(new Date(last))}` : count;
	}
</script>

<div class="list">
	<h1>{t('agent.projects')}</h1>
	{#each projects as p (p.id)}
		<button type="button" class="row" onclick={() => onopen(p)}>
			<span class="ic" aria-hidden="true">{p.icon || '📁'}</span>
			<span class="text"><strong>{p.name}</strong><small>{sub(p)}</small></span>
			<Icon name="chevronRight" size={16} />
		</button>
	{:else}
		<p class="muted empty">{t('agent.tabs.noProjects')}</p>
	{/each}
	<button type="button" class="new" onclick={onnew}
		><Icon name="plus" size={18} />{t('agent.newProject')}</button
	>
</div>

<style>
	.list {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: calc(var(--agent-top, 0px) + 64px) var(--space-3) var(--space-4);
	}
	h1 {
		margin: 0 var(--space-1-5) var(--space-2);
		font-size: 1.35rem;
		font-weight: 600;
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 64px;
		padding: var(--space-2) var(--space-1-5);
		border: 0;
		border-radius: 16px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.row:focus-visible,
	.new:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.row :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.ic {
		flex: none;
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border-radius: 12px;
		background: color-mix(in srgb, var(--text) 7%, var(--surface));
		font-size: 20px;
	}
	.text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.3;
	}
	.text strong {
		font-size: var(--text-lg);
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.text small {
		color: var(--muted);
		font-size: var(--text-md);
	}
	.empty {
		margin: var(--space-4) var(--space-1-5);
	}
	.new {
		align-self: flex-end;
		position: sticky;
		bottom: var(--space-2);
		margin-top: auto;
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 48px;
		padding: 0 var(--space-5);
		border: 0;
		border-radius: 999px;
		background: var(--text);
		color: var(--bg);
		font: inherit;
		font-weight: 500;
		cursor: pointer;
		box-shadow: var(--shadow-menu, 0 8px 24px rgb(0 0 0 / 0.35));
	}
</style>
