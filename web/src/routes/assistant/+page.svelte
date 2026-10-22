<script lang="ts">
	// The assistant on a page of its own: the chats on the left, the open
	// one on the right. On a phone, the list folds away.
	import { onMount } from 'svelte';
	import { assistant, loadAssistant, loadChats, loadModels, openChat } from '#lib/assistant.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import AssistantChat from '#lib/components/AssistantChat.svelte';
	import AssistantChats from '#lib/components/AssistantChats.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';

	let showList = $state(false);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	onMount(async () => {
		// The panel is not needed while the page shows the chat.
		assistant.open = false;
		if (!assistant.status) await loadAssistant();
		if (!assistant.status?.available) return;
		if (assistant.models.length === 0) loadModels();
		await loadChats();
		if (!assistant.chatId && assistant.chats.length > 0) await openChat(assistant.chats[0].id);
		assistant.view = 'chat';
	});
</script>

<PageHead title={t('assistant.title')}>
	{#snippet actions()}
		{#if assistant.status?.available}
			<button type="button" class="btn list-toggle" aria-pressed={showList} onclick={() => (showList = !showList)}><Icon name="list" size={14} />{t('assistant.chats')}</button>
			{#if admin}<a class="btn" href="/settings/assistant"><Icon name="tool" size={14} />{t('assistant.settings')}</a>{/if}
		{/if}
	{/snippet}
</PageHead>

{#if assistant.status && !assistant.status.available}
	<EmptyState title={t('assistant.unavailable')} text={t(admin ? 'assistant.unavailableAdmin' : 'assistant.unavailableUser')}>
		{#if admin}<a class="btn btn-primary" href="/settings/assistant">{t('assistant.settings')}</a>{/if}
	</EmptyState>
{:else if assistant.status}
	<section class="tile layout" class:list={showList}>
		<div class="side"><AssistantChats onopen={() => (showList = false)} /></div>
		<div class="main">
			<h2 class="title" title={assistant.title}>{assistant.title || t('assistant.newChat')}</h2>
			<AssistantChat />
		</div>
	</section>
{/if}

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(220px, 300px) 1fr;
		height: calc(100dvh - 210px);
		min-height: 440px;
		padding: 0;
		overflow: hidden;
	}
	.side {
		display: flex;
		flex-direction: column;
		min-height: 0;
		border-right: 1px solid var(--border);
	}
	.main {
		display: flex;
		flex-direction: column;
		min-width: 0;
		min-height: 0;
	}
	.title {
		margin: 0;
		padding: 12px 14px;
		border-bottom: 1px solid var(--border);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.btn {
		gap: 6px;
	}
	.list-toggle {
		display: none;
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: 1fr;
			height: calc(100dvh - 180px);
		}
		.side {
			display: none;
			border-right: 0;
		}
		.layout.list .side {
			display: flex;
		}
		.layout.list .main {
			display: none;
		}
		.list-toggle {
			display: inline-flex;
		}
	}
</style>
