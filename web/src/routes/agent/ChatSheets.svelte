<script lang="ts">
	// The sheets and small dialogs around one chat of the KI-Agent (actions,
	// archive, move, rename, delete): split off its page (PLAN 75-5).
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { ChatInfo, Project } from '#lib/agent.ts';
	import type { ChatSheet } from './chat-sheets.ts';

	let {
		sheet,
		chats,
		projects,
		keepPinned = $bindable(),
		set,
		onopen,
		onpatch,
		onremove,
		onremoveall,
	}: {
		sheet: ChatSheet;
		chats: ChatInfo[];
		projects: Project[];
		keepPinned: boolean;
		/** Shows another sheet, or none. */
		set: (next: ChatSheet | null) => void;
		onopen: (id: string) => Promise<void> | void;
		onpatch: (chat: ChatInfo, body: Record<string, unknown>) => Promise<void> | void;
		onremove: (chat: ChatInfo) => Promise<void> | void;
		onremoveall: () => Promise<void> | void;
	} = $props();

	const close = () => set(null);
	// The new name is typed here, not into the parent's state.
	let title = $state('');
	$effect.pre(() => {
		if (sheet.kind === 'rename') title = sheet.title;
	});

	function sheetFocus(node: HTMLElement) {
		const back = document.activeElement as HTMLElement | null;
		node.querySelector<HTMLElement>('button')?.focus();
		const trap = (e: KeyboardEvent) => {
			if (e.key !== 'Tab') return;
			const items = [...node.querySelectorAll<HTMLElement>('button:not(:disabled)')];
			if (!items.length) return;
			const first = items[0];
			const last = items[items.length - 1];
			if (e.shiftKey && document.activeElement === first) {
				e.preventDefault();
				last.focus();
			} else if (!e.shiftKey && document.activeElement === last) {
				e.preventDefault();
				first.focus();
			}
		};
		node.addEventListener('keydown', trap);
		return { destroy: () => (node.removeEventListener('keydown', trap), back?.focus?.()) };
	}
</script>

{#if sheet.kind === 'actions'}
	{@const chat = sheet.chat}
	<!-- PLAN 53: everything you can do with a chat, as a sheet from below. -->
	<div class="sheet-scrim" role="presentation" onclick={() => close()}></div>
	<div
		class="action-sheet"
		role="dialog"
		aria-modal="true"
		aria-label={chat.title}
		tabindex="-1"
		use:sheetFocus
		onkeydown={(e) => {
			if (e.key === 'Escape') close();
		}}
	>
		<span class="grab" aria-hidden="true"></span>
		<strong class="sheet-title">{chat.title || t('agent.newChat')}</strong>
		<div class="sheet-list">
			<button
				type="button"
				onclick={() => {
					close();
					void onpatch(chat, { pinned: !chat.pinned });
				}}
				><span class="sq"><Icon name="pin" size={17} /></span>{chat.pinned
					? t('agent.unpin')
					: t('agent.pin')}</button
			>
			<button type="button" onclick={() => set({ kind: 'rename', chat, title: chat.title })}
				><span class="sq"><Icon name="edit" size={17} /></span>{t('agent.rename')}</button
			>
			<button type="button" onclick={() => set({ kind: 'move', chat })}
				><span class="sq"><Icon name="folder" size={17} /></span>{t('agent.moveToProject')}</button
			>
			<button
				type="button"
				onclick={() => {
					close();
					void onpatch(chat, { archived: !chat.archived });
				}}
				><span class="sq"><Icon name="archive" size={17} /></span>{chat.archived
					? t('agent.archive.restore')
					: t('agent.archive.do')}</button
			>
			<a href={`/api/v1/agent/chats/${chat.id}/export`} download onclick={() => close()}
				><span class="sq"><Icon name="download" size={17} /></span>{t('agent.exportMd')}</a
			>
			<a href={`/api/v1/agent/chats/${chat.id}/export?format=pdf`} download onclick={() => close()}
				><span class="sq"><Icon name="fileText" size={17} /></span>{t('agent.exportPdf')}</a
			>
			<button type="button" class="danger" onclick={() => set({ kind: 'delete', chat })}
				><span class="sq"><Icon name="trash" size={17} /></span>{t('agent.delete')}</button
			>
		</div>
	</div>
{:else if sheet.kind === 'archive'}
	{@const list = chats
		.filter((c) => c.archived && !c.author)
		.sort((a, b) => b.updated.localeCompare(a.updated))}
	<Dialog title={t('agent.archive.title')} onclose={() => close()}>
		<section class="tile ui-card archive-list">
			{#each list as c (c.id)}
				<div class="set-row">
					<span class="set-label"
						>{c.title || t('agent.newChat')}<span class="set-hint"
							>{formatDateTime(new Date(c.updated))}</span
						></span
					>
					<span class="set-control">
						<button
							type="button"
							class="btn"
							onclick={() => {
								close();
								void onopen(c.id);
							}}>{t('agent.archive.open')}</button
						>
						<button type="button" class="btn" onclick={() => void onpatch(c, { archived: false })}
							>{t('agent.archive.restore')}</button
						>
					</span>
				</div>
			{:else}
				<p class="set-row muted">{t('agent.archive.empty')}</p>
			{/each}
		</section>
	</Dialog>
{:else if sheet.kind === 'move'}
	{@const chat = sheet.chat}
	<Dialog title={t('agent.moveToProject')} onclose={() => close()}>
		<div class="choose">
			<button
				type="button"
				class="btn"
				onclick={() => {
					close();
					void onpatch(chat, { project: '' });
				}}>{t('agent.noProject')}</button
			>
			{#each projects as p (p.id)}
				<button
					type="button"
					class="btn"
					class:btn-primary={chat.project === p.id}
					onclick={() => {
						close();
						void onpatch(chat, { project: p.id });
					}}>{p.icon ?? ''} {p.name}</button
				>
			{/each}
		</div>
	</Dialog>
{:else if sheet.kind === 'rename'}
	{@const d = sheet}
	<Dialog title={t('agent.rename')} onclose={() => close()}>
		<form
			class="choose"
			onsubmit={(e) => {
				e.preventDefault();
				close();
				void onpatch(d.chat, { title });
			}}
		>
			<input
				class="field"
				bind:value={title}
				maxlength="200"
				required
				aria-label={t('agent.rename')}
			/>
			<div class="row">
				<button type="button" class="btn" onclick={() => close()}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary">{t('common.save')}</button>
			</div>
		</form>
	</Dialog>
{:else if sheet.kind === 'delete'}
	{@const chat = sheet.chat}
	<Dialog title={t('agent.deleteTitle')} onclose={() => close()}>
		<p>{t('agent.deleteText', { title: chat.title })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={() => onremove(chat)}
				>{t('common.delete')}</button
			>
			<button type="button" class="btn" onclick={() => close()}>{t('common.cancel')}</button>
		</div>
	</Dialog>
{:else if sheet.kind === 'deleteAll'}
	{@const own = chats.filter((c) => !c.author)}
	{@const pinned = own.filter((c) => c.pinned).length}
	<Dialog title={t('agent.deleteAllTitle')} onclose={() => close()}>
		<p>{t('agent.deleteAllText', { count: own.length })}</p>
		{#if pinned}
			<label class="check"
				><input type="checkbox" bind:checked={keepPinned} />
				<span>{t('agent.deleteAllKeepPinned', { count: pinned })}</span></label
			>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={onremoveall}
				>{t('agent.deleteAllButton')}</button
			>
			<button type="button" class="btn" onclick={() => close()}>{t('common.cancel')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.sheet-scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.5);
	}
	.action-sheet {
		position: fixed;
		z-index: 71;
		left: 0;
		right: 0;
		bottom: 0;
		max-width: 520px;
		margin: 0 auto;
		padding: var(--space-2) 14px calc(env(safe-area-inset-bottom, 0px) + 18px);
		border-radius: 18px 18px 0 0;
		color: var(--text);
		background: var(--bg);
		border-top: 1px solid var(--border);
		animation: sheet-up 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	@keyframes sheet-up {
		from {
			transform: translateY(100%);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.action-sheet {
			animation: none;
		}
	}
	.grab {
		display: block;
		width: 44px;
		height: 5px;
		border-radius: 3px;
		margin: 2px auto var(--space-2-5);
		background: color-mix(in srgb, var(--text) 25%, transparent);
	}
	.sheet-title {
		display: block;
		padding: 0 var(--space-1-5) var(--space-2-5);
		font-size: var(--text-md);
	}
	.sheet-list {
		border-radius: var(--radius-tile);
		overflow: hidden;
		background: var(--surface);
		border: 1px solid var(--border);
	}
	.sheet-list > * {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 54px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-decoration: none;
		text-align: left;
		cursor: pointer;
	}
	.sheet-list > :first-child {
		border-top: 0;
	}
	.sheet-list .danger {
		color: var(--error-text);
	}
	.sheet-list .sq {
		width: 32px;
		height: 32px;
		border-radius: var(--radius-ctl);
		display: grid;
		place-items: center;
		color: var(--muted);
		background: var(--chip);
	}
	.choose {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.choose .row {
		display: flex;
		gap: var(--space-2);
		justify-content: flex-end;
	}
	.dialog-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: var(--space-3);
	}
</style>
