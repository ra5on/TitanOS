<script lang="ts">
	// A message of the person: a bubble on the right, at the desktop with
	// "Bearbeiten" and "Kopieren" under it; on the phone a long press opens
	// its actions (onhold). While it is changed, a field takes its place
	// ("Bearbeiten und erneut senden", PLAN 90-4).
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import type { Copied } from './chat-text.svelte.ts';

	let {
		entry,
		editing = $bindable(null),
		phone = false,
		canChange = false,
		copied,
		onhold,
		onresend,
	}: {
		entry: Entry;
		/** The message being changed, with its new text (one at a time). */
		editing?: { id: string; text: string } | null;
		phone?: boolean;
		/** It can be changed and asked again (nothing runs, not read-only). */
		canChange?: boolean;
		copied: Copied;
		/** The actions of the message (a long press on the phone). */
		onhold: (entry: Entry) => void;
		onresend: (entry: Entry, text: string) => void;
	} = $props();

	const done = $derived(copied.of(entry.id));
	/** A message sent a moment ago: it comes in like the line that follows
	 *  it instead of standing there at once (0.100, look through the chat). */
	const fresh = (e: Entry) => Date.now() - Date.parse(e.at) < 4000;

	function resend() {
		const text = editing?.text.trim() ?? '';
		if (!text) return;
		editing = null;
		onresend(entry, text);
	}
	function editArea(node: HTMLTextAreaElement) {
		node.focus();
		node.setSelectionRange(node.value.length, node.value.length);
	}
	// On the phone a long press on the own message opens its actions.
	function hold(node: HTMLElement) {
		let timer: ReturnType<typeof setTimeout> | undefined;
		let at = { x: 0, y: 0 };
		const clear = () => clearTimeout(timer);
		const down = (e: PointerEvent) => {
			if (e.pointerType !== 'touch') return;
			at = { x: e.clientX, y: e.clientY };
			clear();
			timer = setTimeout(() => onhold(entry), 500);
		};
		const move = (e: PointerEvent) => {
			if (Math.hypot(e.clientX - at.x, e.clientY - at.y) > 8) clear();
		};
		const menu = (e: Event) => {
			if (!phone) return;
			e.preventDefault();
			clear();
			onhold(entry);
		};
		node.addEventListener('pointerdown', down);
		node.addEventListener('pointermove', move);
		node.addEventListener('pointerup', clear);
		node.addEventListener('pointercancel', clear);
		node.addEventListener('contextmenu', menu);
		return {
			destroy() {
				clear();
				node.removeEventListener('pointerdown', down);
				node.removeEventListener('pointermove', move);
				node.removeEventListener('pointerup', clear);
				node.removeEventListener('pointercancel', clear);
				node.removeEventListener('contextmenu', menu);
			},
		};
	}
</script>

{#if editing?.id === entry.id}
	<form
		class="editing"
		onsubmit={(e) => {
			e.preventDefault();
			resend();
		}}
	>
		<textarea
			rows="3"
			maxlength="20000"
			bind:value={editing.text}
			use:editArea
			aria-label={t('agent.editMessage')}
			onkeydown={(e) => {
				if (e.key === 'Escape') editing = null;
				else if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
					e.preventDefault();
					resend();
				}
			}}></textarea>
		<div class="edit-foot">
			<span>{t('agent.editHint')}</span>
			<button type="button" class="btn" onclick={() => (editing = null)}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={!editing.text.trim()}
				>{t('agent.resend')}</button
			>
		</div>
	</form>
{:else}
	<div class="me">
		<div class="user" class:fresh={fresh(entry)} use:hold>
			<span>{entry.content}</span>
		</div>
		{#if !phone}
			<div class="acts">
				{#if canChange}
					<button
						type="button"
						class="act"
						onclick={() => (editing = { id: entry.id, text: entry.content ?? '' })}
						><Icon name="edit" size={14} /><span>{t('agent.edit')}</span></button
					>
				{/if}
				<button type="button" class="act" onclick={() => copied.copy(entry.id, entry.content ?? '')}
					><Icon name={done?.ok ? 'check' : 'copy'} size={14} /><span
						>{done
							? done.ok
								? t('agent.copiedShort')
								: t('agent.copyFailed')
							: t('agent.copy')}</span
					></button
				>
			</div>
		{/if}
	</div>
{/if}

<style>
	.me {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 2px;
		margin: var(--space-1) 0;
	}
	.user {
		display: flex;
		justify-content: flex-end;
		width: 100%;
	}
	.user span {
		/* 1.07: with the full width of the chat the bubble stays a bubble. */
		max-width: 70%;
		padding: 10px 16px;
		border-radius: 20px;
		background: color-mix(in srgb, var(--text) 9%, var(--surface));
		color: var(--text);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.user.fresh {
		animation: come 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
	}
	@keyframes come {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
	:global(.agent.calm) .user.fresh {
		animation: none;
	}
	/* Changing an own message: a field in its place. */
	.editing {
		padding: var(--space-3);
		border: 1px solid var(--accent);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	.editing textarea {
		display: block;
		width: 100%;
		min-height: 64px;
		border: 0;
		outline: 0;
		resize: vertical;
		background: none;
		color: var(--text);
		font: inherit;
		line-height: 1.5;
	}
	.edit-foot {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.edit-foot span {
		flex: 1;
		min-width: 160px;
	}
	/* The small row under the message: Bearbeiten, Kopieren. */
	.acts {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-1-5);
		margin: 0;
		font-size: var(--text-sm);
		color: var(--muted);
		animation: settle 0.35s ease-out both;
	}
	@keyframes settle {
		from {
			opacity: 0;
		}
	}
	.act {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		justify-content: center;
		min-height: 32px;
		min-width: 32px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		cursor: pointer;
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	.act:hover {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.act:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	:global(.agent.calm) .act {
		transition: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.user.fresh,
		.acts {
			animation: none;
		}
	}
	@media (max-width: 760px) {
		.act {
			min-height: 44px;
			min-width: 44px;
		}
		.user span {
			max-width: 86%;
			/* A long press opens the actions, it does not select text. */
			user-select: none;
			-webkit-user-select: none;
			-webkit-touch-callout: none;
		}
		.edit-foot .btn {
			min-height: 44px;
		}
	}
</style>
