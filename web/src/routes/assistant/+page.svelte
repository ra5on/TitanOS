<script lang="ts">
	// The assistant on a page of its own: the chats on the left, the open
	// one on the right. On a phone, the list folds away.
	import { page } from '$app/state';
	import { onMount, untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import {
		assistant,
		loadChats,
		loadModels,
		openChat,
		type AssistantStatus,
	} from '#lib/assistant.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import { session } from '#lib/session.svelte.ts';
	import AssistantChat from '#lib/components/AssistantChat.svelte';
	import AssistantChats from '#lib/components/AssistantChats.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';

	let showList = $state(false);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	let loadError = $state(''),
		loading = $state(false),
		ready = $state(false);
	const requested = $derived(page.url.searchParams.get('chat'));

	// A failed status request is an error with a retry, not "unavailable".
	async function start() {
		loading = true;
		loadError = '';
		try {
			if (!assistant.status?.available)
				assistant.status = await api.get<AssistantStatus>('/assistant/status');
		} catch (err) {
			loadError = apiErrorText(err);
			return;
		} finally {
			loading = false;
		}
		if (!assistant.status?.available) return;
		if (assistant.models.length === 0) loadModels();
		await loadChats();
		ready = true;
		if (requested) return; // opened by the effect below
		// Continue the newest chat, unless one is being started right now.
		if (
			!assistant.chatId &&
			!assistant.busy &&
			assistant.messages.length === 0 &&
			assistant.chats.length > 0
		)
			await openChat(assistant.chats[0].id);
	}
	onMount(() => void start());
	// A link with ?chat= opens that chat, also while the page is already open.
	$effect(() => {
		const id = requested;
		if (!ready || !id) return;
		untrack(() => {
			if (id !== assistant.chatId) void openChat(id);
		});
	});
</script>

<PageHead title={t('assistant.title')}>
	{#snippet actions()}
		{#if assistant.status?.available}
			<span class="wide-links">
				<a class="btn" href="/assistant/tasks">{t('tasksAI.title')}</a>
				<a class="btn" href="/assistant/knowledge">{t('knowledge.title')}</a>
				<a class="btn" href="/assistant/automations">{t('automation.title')}</a>
			</span>
			<span class="more-links">
				<Dropdown label={t('assistant.more')} text={t('assistant.more')} icon="more" align="right">
					{#snippet children(close)}
						<a role="menuitem" href="/assistant/tasks" onclick={close}
							><span>{t('tasksAI.title')}</span></a
						>
						<a role="menuitem" href="/assistant/knowledge" onclick={close}
							><span>{t('knowledge.title')}</span></a
						>
						<a role="menuitem" href="/assistant/automations" onclick={close}
							><span>{t('automation.title')}</span></a
						>
					{/snippet}
				</Dropdown>
			</span>
			<button
				type="button"
				class="btn list-toggle"
				aria-pressed={showList}
				onclick={() => (showList = !showList)}
				><Icon name="list" size={14} />{t('assistant.chats')}</button
			>
			{#if admin}<a class="btn" href="/settings/assistant"
					><Icon name="tool" size={14} />{t('assistant.settings')}</a
				>{/if}
		{/if}
	{/snippet}
</PageHead>

{#if loadError}
	<div class="notice-error load-error" role="alert">
		<span>{loadError}</span>
		<button type="button" class="btn" disabled={loading} onclick={start}>{t('common.retry')}</button
		>
	</div>
{:else if !assistant.status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else if assistant.status && !assistant.status.available}
	<EmptyState
		title={t('assistant.unavailable')}
		text={t(admin ? 'assistant.unavailableAdmin' : 'assistant.unavailableUser')}
	>
		{#if admin}<a class="btn btn-primary" href="/settings/assistant">{t('assistant.settings')}</a
			>{/if}
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
	.wide-links {
		display: contents;
	}
	.more-links {
		display: none;
	}
	.load-error {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
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
		.wide-links {
			display: none;
		}
		.more-links {
			display: contents;
		}
	}
</style>
