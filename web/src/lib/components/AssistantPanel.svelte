<script lang="ts">
	// The chat with the assistant at the right edge of every page. Its width
	// is dragged at the left edge and kept for the account.
	import { page } from '$app/state';
	import { assistant, newChat, setPrefs } from '#lib/assistant.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import AssistantChat from './AssistantChat.svelte';
	import AssistantChats from './AssistantChats.svelte';
	import Icon from './Icon.svelte';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	// On its own page the chat needs no panel.
	const shown = $derived(assistant.open && page.url.pathname !== '/assistant');

	let width = $state(assistant.prefs.width);
	$effect(() => {
		width = assistant.prefs.width;
	});

	function drag(event: PointerEvent) {
		event.preventDefault();
		const handle = event.currentTarget as HTMLElement;
		handle.setPointerCapture(event.pointerId);
		const move = (e: PointerEvent) => {
			width = Math.round(Math.min(Math.max(window.innerWidth - e.clientX, 320), Math.min(1200, window.innerWidth)));
		};
		const up = () => {
			handle.removeEventListener('pointermove', move);
			handle.removeEventListener('pointerup', up);
			setPrefs({ width });
		};
		handle.addEventListener('pointermove', move);
		handle.addEventListener('pointerup', up);
	}

	function key(event: KeyboardEvent) {
		const step = event.key === 'ArrowLeft' ? 40 : event.key === 'ArrowRight' ? -40 : 0;
		if (!step) return;
		event.preventDefault();
		width = Math.min(Math.max(width + step, 320), 1200);
		setPrefs({ width });
	}
</script>

<svelte:window onkeydown={(event) => { if (event.key === 'Escape' && shown && !event.defaultPrevented && !document.querySelector('dialog[open]')) assistant.open = false; }} />

{#if shown}
	<aside class="panel" style:width="min({width}px, 100vw)" aria-label={t('assistant.title')}>
		<!-- A separator that can be moved is a widget: it takes the focus and arrow keys. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<div class="resize" role="separator" aria-orientation="vertical" aria-valuenow={width} aria-valuemin={320} aria-valuemax={1200} aria-label={t('assistant.resize')} tabindex="0" onpointerdown={drag} onkeydown={key}></div>
		<div class="head">
			<button type="button" class="icon" aria-pressed={assistant.view === 'list'} aria-label={t('assistant.chats')} title={t('assistant.chats')} onclick={() => (assistant.view = assistant.view === 'list' ? 'chat' : 'list')}><Icon name="list" size={16} /></button>
			<h2 title={assistant.title}>{assistant.view === 'list' ? t('assistant.chats') : assistant.title || t('assistant.title')}</h2>
			<span class="grow"></span>
			<button type="button" class="icon" aria-label={t('assistant.new')} title={t('assistant.new')} onclick={newChat} disabled={assistant.busy || (assistant.view === 'chat' && assistant.messages.length === 0)}><Icon name="plus" size={16} /></button>
			<a class="icon" href="/assistant" aria-label={t('assistant.openPage')} title={t('assistant.openPage')}><Icon name="maximize" size={16} /></a>
			{#if admin}<a class="icon" href="/settings/assistant" aria-label={t('assistant.settings')} title={t('assistant.settings')}><Icon name="tool" size={16} /></a>{/if}
			<button type="button" class="icon" aria-label={t('assistant.close')} title={t('assistant.close')} onclick={() => (assistant.open = false)}><Icon name="close" size={16} /></button>
		</div>
		{#if assistant.view === 'list'}
			<AssistantChats />
		{:else}
			<AssistantChat />
		{/if}
	</aside>
{/if}

<style>
	.panel {
		position: fixed;
		top: 60px;
		right: 0;
		bottom: 0;
		z-index: 30;
		display: flex;
		flex-direction: column;
		background: var(--surface);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu);
	}
	.resize {
		position: absolute;
		top: 0;
		bottom: 0;
		left: -4px;
		width: 8px;
		cursor: ew-resize;
		touch-action: none;
		z-index: 1;
	}
	.resize:focus-visible {
		outline: 2px solid var(--accent);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 10px 12px;
		border-bottom: 1px solid var(--border);
	}
	h2 {
		margin: 0;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.grow {
		flex: 1;
	}
	.icon {
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
	}
	.icon:hover:not(:disabled),
	.icon[aria-pressed='true'] {
		border-color: var(--border);
	}
	.icon:disabled {
		opacity: 0.45;
		cursor: default;
	}
	@media (max-width: 760px) {
		.resize {
			display: none;
		}
	}
</style>
