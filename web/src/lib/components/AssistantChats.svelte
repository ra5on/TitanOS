<script lang="ts">
	// The chats of this account: search, open, rename, delete.
	import { onMount } from 'svelte';
	import { assistant, deleteAllChats, deleteChat, loadChats, newChat, openChat, renameChat, setPrefs } from '#lib/assistant.svelte.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { ApiError } from '#lib/api.ts';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';

	let { onopen }: { onopen?: () => void } = $props();

	let renaming = $state<{ id: string; title: string } | null>(null);
	let removing = $state<{ id: string; title: string } | 'all' | null>(null);
	let error = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	onMount(() => {
		loadChats();
	});

	function search(event: Event) {
		assistant.search = (event.currentTarget as HTMLInputElement).value;
		clearTimeout(timer);
		timer = setTimeout(loadChats, 250);
	}

	function open(id: string) {
		openChat(id);
		onopen?.();
	}

	async function act(work: () => Promise<void>) {
		error = '';
		try {
			await work();
			renaming = null;
			removing = null;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	const when = (iso: string) => formatDateTime(new Date(iso));
</script>

<div class="chats">
	<div class="top">
		<input class="field" type="search" value={assistant.search} oninput={search} placeholder={t('assistant.searchChats')} aria-label={t('assistant.searchChats')} />
		<button type="button" class="btn btn-primary" onclick={() => { newChat(); onopen?.(); }}><Icon name="plus" size={14} />{t('assistant.new')}</button>
	</div>
	{#if !assistant.status?.persistent}<p class="notice-warn small" role="note">{t('assistant.memoryOnly')}</p>{/if}
	<ul>
		{#each assistant.chats as chat (chat.id)}
			<li class:current={chat.id === assistant.chatId}>
				<button type="button" class="open" onclick={() => open(chat.id)}>
					<span class="title">{chat.title || t('assistant.untitled')}</span>
					<span class="muted small">{when(chat.updated)}</span>
				</button>
				<button type="button" class="mini" aria-label={t('assistant.rename')} title={t('assistant.rename')} onclick={() => (renaming = { id: chat.id, title: chat.title })}><Icon name="edit" size={14} /></button>
				<button type="button" class="mini" aria-label={t('assistant.deleteChat')} title={t('assistant.deleteChat')} onclick={() => (removing = { id: chat.id, title: chat.title })} disabled={assistant.busy && chat.id === assistant.chatId}><Icon name="trash" size={14} /></button>
			</li>
		{:else}
			<li class="none muted">{t(assistant.search ? 'assistant.noMatches' : 'assistant.noChats')}</li>
		{/each}
	</ul>
	{#if assistant.chats.length > 1 && !assistant.search}
		<button type="button" class="btn all" onclick={() => (removing = 'all')} disabled={assistant.busy}>{t('assistant.deleteAll')}</button>
	{/if}
	<label class="pref">
		<input type="checkbox" checked={assistant.prefs.floating} onchange={(event) => setPrefs({ floating: event.currentTarget.checked })} />
		<span class="muted">{t('assistant.floating')}</span>
	</label>
</div>

{#if renaming}
	<Dialog title={t('assistant.rename')} onclose={() => (renaming = null)}>
		<form onsubmit={(event) => { event.preventDefault(); const r = renaming!; act(() => renameChat(r.id, r.title)); }}>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="field" bind:value={renaming.title} maxlength="120" aria-label={t('assistant.chatTitle')} required autofocus />
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (renaming = null)}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary">{t('assistant.save')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={removing === 'all' ? t('assistant.deleteAll') : t('assistant.deleteChat')} onclose={() => (removing = null)}>
		<p>{removing === 'all' ? t('assistant.deleteAllText') : t('assistant.deleteChatText', { title: removing.title || t('assistant.untitled') })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" onclick={() => { const r = removing!; act(() => (r === 'all' ? deleteAllChats() : deleteChat(r.id))); }}>{t('assistant.delete')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.chats {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
		min-height: 0;
		overflow-y: auto;
		font-size: 0.875rem;
	}
	.top {
		display: flex;
		gap: 8px;
	}
	.top input {
		flex: 1;
		min-width: 0;
	}
	.top .btn {
		gap: 6px;
	}
	ul {
		display: grid;
		gap: 2px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	li {
		display: flex;
		align-items: center;
		gap: 2px;
		border-radius: var(--radius-ctl);
	}
	li.current {
		background: var(--chip);
	}
	li:hover {
		background: var(--chip);
	}
	.open {
		flex: 1;
		min-width: 0;
		display: grid;
		gap: 2px;
		padding: 8px 10px;
		background: transparent;
		border: 0;
		color: var(--text);
		text-align: left;
		cursor: pointer;
	}
	.title {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.small {
		margin: 0;
		font-size: 0.75rem;
	}
	.mini {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 30px;
		height: 30px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-small);
		color: var(--muted);
		cursor: pointer;
	}
	.mini:hover:not(:disabled) {
		border-color: var(--border);
		color: var(--text);
	}
	.none {
		padding: 8px 10px;
	}
	.all {
		align-self: flex-start;
	}
	form {
		display: grid;
		gap: 12px;
	}
	.pref {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: auto;
		padding-top: 8px;
		font-size: 0.75rem;
	}
</style>
