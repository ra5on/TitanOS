<script lang="ts" module>
	export type EntryAction = 'archive' | 'delete' | 'seen' | 'unseen' | 'flag' | 'unflag';
	/** 1.15: the swipe's "Mehr" opens the list's menu for this entry. */
	export type EntryMore = 'more';
</script>

<script lang="ts">
	// 1.12: one entry of the list – a mail or a conversation. Sender
	// picture, sender, marks and date, subject, the preview titand cut at a
	// word (wrapped, never clamped). Beside the button that opens it: a box
	// to select it, the count of a conversation that opens it, and the quick
	// actions (on hover and focus; on the phone a swipe). Unread looks as
	// before (bold with a dot and the word for screen readers).
	import Icon from '../components/Icon.svelte';
	import SwipeRow, { type SwipeAction } from '../components/SwipeRow.svelte';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import SenderAvatar from './SenderAvatar.svelte';
	import { listDate, senderName } from './text.ts';
	import type { Row } from './threads.ts';

	type Props = {
		row: Row;
		/** All mails of the conversation (row is the newest); one for a mail. */
		rows: Row[];
		child?: boolean;
		expanded?: boolean;
		current?: boolean;
		picked?: boolean;
		phone?: boolean;
		inTrash?: boolean;
		/** Archiving makes no sense in the archive itself. */
		canArchive?: boolean;
		/** The folder of a hit of the search over all folders. */
		folderName?: string;
		draggable?: boolean;
		/** 1.15 (drafts 16–17): the round boxes show only while selecting. */
		editing?: boolean;
		/** 1.15: compact views show the arrow after the date (Apple Mail). */
		arrow?: boolean;
		/** 1.15: a long press (touch) starts selecting with this entry. */
		onlongpress?: () => void;
		/** 1.15: the swipe's "Mehr". */
		onmore?: () => void;
		onopen: (e: MouseEvent) => void;
		onpick: (e: MouseEvent) => void;
		ontoggle?: () => void;
		onaction: (kind: EntryAction) => void;
		ondragstart?: (e: DragEvent) => void;
		ondragend?: () => void;
	};
	let {
		row,
		rows,
		child = false,
		expanded = false,
		current = false,
		picked = false,
		phone = false,
		inTrash = false,
		canArchive = true,
		folderName = '',
		draggable = false,
		editing = false,
		arrow = false,
		onlongpress,
		onmore,
		onopen,
		onpick,
		ontoggle,
		onaction,
		ondragstart,
		ondragend,
	}: Props = $props();

	const all = $derived(child ? [row] : rows);
	const unread = $derived(all.some((r) => r.flags?.seen === false));
	const flagged = $derived(all.some((r) => r.flags?.flagged));
	const attachment = $derived(all.some((r) => r.attachments));
	const count = $derived(child ? 1 : rows.length);
	const name = $derived(senderName(row.from) || t('mail.noSender'));
	const subject = $derived(row.subject?.trim() || t('mail.noSubject'));
	const deleteLabel = $derived(inTrash ? t('mail.action.deleteForever') : t('mail.action.delete'));

	// 1.15 (draft 19): to the left Mehr · Markieren · Löschen, to the right
	// read/unread (Apple Mail); archiving is in "Mehr".
	const swipe = $derived<SwipeAction[]>([
		{
			id: 'delete',
			label: deleteLabel,
			icon: 'trash',
			danger: true,
			run: () => onaction('delete'),
		},
		{
			id: 'flag',
			label: flagged ? t('mail.action.unflag') : t('mail.action.flag'),
			icon: 'flag',
			run: () => onaction(flagged ? 'unflag' : 'flag'),
		},
		...(onmore
			? [{ id: 'more', label: t('mail.action.more'), icon: 'more' as const, run: () => onmore?.() }]
			: []),
	]);
	const leading = $derived<SwipeAction | undefined>({
		id: 'seen',
		label: unread ? t('mail.action.markRead') : t('mail.action.markUnread'),
		icon: 'mail',
		run: () => onaction(unread ? 'seen' : 'unseen'),
	});

	// A long press with a finger selects (and starts selecting); the click
	// that follows it opens nothing.
	let pressTimer: ReturnType<typeof setTimeout> | undefined;
	let pressAt: { x: number; y: number } | null = null;
	let pressed = false;
	function pressStart(e: PointerEvent) {
		if (!onlongpress || e.pointerType === 'mouse' || editing) return;
		pressed = false;
		pressAt = { x: e.clientX, y: e.clientY };
		clearTimeout(pressTimer);
		pressTimer = setTimeout(() => {
			pressed = true;
			navigator.vibrate?.(10);
			onlongpress?.();
		}, 500);
	}
	function pressMove(e: PointerEvent) {
		if (pressAt && Math.hypot(e.clientX - pressAt.x, e.clientY - pressAt.y) > 10) pressEnd();
	}
	function pressEnd() {
		clearTimeout(pressTimer);
		pressAt = null;
	}
	function open(e: MouseEvent) {
		if (pressed) {
			pressed = false;
			e.preventDefault();
			return;
		}
		if (editing) {
			onpick(e);
			return;
		}
		onopen(e);
	}
</script>

{#snippet body()}
	<!-- Dragging is the mouse's way to move; the bulk bar's "Verschieben" is the keyboard's. -->
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="entry"
		class:child
		class:current
		class:picked
		data-uid={row.uid}
		data-folder={row.folder}
		draggable={draggable ? 'true' : undefined}
		{ondragstart}
		{ondragend}
	>
		<!-- The box and the count of a conversation stand left, apart from the quick actions. -->
		<div class="side" class:empty={!editing && !(count > 1 && ontoggle)}>
			{#if editing}
				<input
					class="pick"
					type="checkbox"
					checked={picked}
					aria-label={t('mail.pick.one', { name: `${name} – ${subject}` })}
					onclick={onpick}
				/>
			{/if}
			{#if count > 1 && ontoggle}
				<button
					type="button"
					class="count"
					aria-expanded={expanded}
					aria-label={`${t('mail.thread.expand')}: ${t('mail.thread.count', { count })}`}
					title={t('mail.thread.count', { count })}
					onclick={ontoggle}
					><span aria-hidden="true">{count}</span><Icon
						name={expanded ? 'chevronDown' : 'chevronRight'}
						size={14}
					/></button
				>
			{/if}
		</div>
		<button
			type="button"
			class="row"
			class:unread
			aria-current={current ? 'true' : undefined}
			data-uid={row.uid}
			onclick={open}
			onpointerdown={pressStart}
			onpointermove={pressMove}
			onpointerup={pressEnd}
			onpointercancel={pressEnd}
			onpointerleave={pressEnd}
			oncontextmenu={(e) => onlongpress && pressAt && e.preventDefault()}
		>
			<SenderAvatar from={row.from} size={child ? 28 : 36} />
			<span class="lines">
				{#if unread && !editing}<span class="dot" aria-hidden="true"></span>{/if}
				<span class="top">
					{#if unread}<span class="sr-only">{t('mail.mark.unread')}</span>{/if}
					<span class="from">{name}</span>
					<span class="marks">
						{#if attachment}<span class="mark" title={t('mail.mark.attachment')}
								><Icon name="clip" size={14} /><span class="sr-only"
									>{t('mail.mark.attachment')}</span
								></span
							>{/if}
						{#if flagged}<span class="mark flagged" title={t('mail.mark.flagged')}
								><Icon name="flag" size={14} /><span class="sr-only">{t('mail.mark.flagged')}</span
								></span
							>{/if}
						{#if row.flags?.answered}<span class="mark" title={t('mail.mark.answered')}
								><Icon name="reply" size={14} /><span class="sr-only"
									>{t('mail.mark.answered')}</span
								></span
							>{/if}
						{#if row.flags?.forwarded}<span class="mark" title={t('mail.mark.forwarded')}
								><Icon name="forward" size={14} /><span class="sr-only"
									>{t('mail.mark.forwarded')}</span
								></span
							>{/if}
					</span>
					<span class="when"
						>{#if row.date}<time datetime={row.date}>{listDate(row.date, i18n.locale)}</time
							>{/if}{#if arrow}<Icon name="chevronRight" size={14} />{/if}</span
					>
				</span>
				{#if row.tooLarge}
					<!-- Its head is too large to list (titand reads only the uid). -->
					<span class="subject large"><Icon name="info" size={14} />{t('mail.list.tooLarge')}</span>
				{:else}
					<span class="subject">{subject}</span>
				{/if}
				{#if row.preview}<span class="preview">{row.preview}</span>{/if}
				{#if folderName}<span class="where"
						><Icon name="folder" size={12} />{t('mail.hit.folder', { folder: folderName })}</span
					>{/if}
			</span>
		</button>
		{#if !phone && !editing}
			<div class="quick" role="group" aria-label={t('mail.quick.label')}>
				{#if canArchive}
					<button
						type="button"
						class="qa"
						title={t('mail.action.archive')}
						aria-label={t('mail.action.archive')}
						onclick={() => onaction('archive')}><Icon name="archive" size={16} /></button
					>
				{/if}
				<button
					type="button"
					class="qa"
					title={deleteLabel}
					aria-label={deleteLabel}
					onclick={() => onaction('delete')}><Icon name="trash" size={16} /></button
				>
				<button
					type="button"
					class="qa"
					title={unread ? t('mail.action.markRead') : t('mail.action.markUnread')}
					aria-label={unread ? t('mail.action.markRead') : t('mail.action.markUnread')}
					onclick={() => onaction(unread ? 'seen' : 'unseen')}
					><Icon name="mail" size={16} /></button
				>
				<button
					type="button"
					class="qa"
					aria-pressed={flagged}
					title={flagged ? t('mail.action.unflag') : t('mail.action.flag')}
					aria-label={flagged ? t('mail.action.unflag') : t('mail.action.flag')}
					onclick={() => onaction(flagged ? 'unflag' : 'flag')}
					><Icon name="flag" size={16} /></button
				>
			</div>
		{/if}
	</div>
{/snippet}

{#if phone}
	<SwipeRow actions={swipe} {leading}>{@render body()}</SwipeRow>
{:else}
	{@render body()}
{/if}

<style>
	/* 1.15 (115-2, drafts 11–19): rows like Apple Mail – the unread dot in
	   the left margin, the sender always bold, date (and in compact views
	   the arrow) at the right with the marks below it, subject and the
	   whole preview; the round boxes only while selecting. */
	.entry {
		position: relative;
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		align-items: start;
		gap: 0;
		padding: 0 var(--space-2) 0 18px;
		border-bottom: 1px solid var(--border);
		background: var(--surface);
	}
	.entry.child {
		padding-left: calc(18px + 28px);
		background: color-mix(in srgb, var(--chip) 40%, var(--surface));
	}
	.entry:hover {
		background: color-mix(in srgb, var(--text) 5%, var(--surface));
	}
	.entry.current {
		background: color-mix(in srgb, var(--accent) 14%, var(--surface));
	}
	.entry.picked {
		background: color-mix(in srgb, var(--accent) 18%, var(--surface));
	}
	.side {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1);
		padding: calc(var(--space-2) + 8px) var(--space-1) 0 0;
	}
	.side.empty {
		display: none;
	}
	.pick {
		appearance: none;
		margin: 0;
		width: 24px;
		height: 24px;
		border-radius: 50%;
		border: 1.8px solid var(--muted);
		background: none;
		cursor: pointer;
		display: grid;
		place-items: center;
	}
	.pick:checked {
		background: var(--accent);
		border-color: var(--accent);
	}
	.pick:checked::after {
		content: '';
		width: 9px;
		height: 5px;
		margin-top: -2px;
		border-left: 2.2px solid var(--on-accent);
		border-bottom: 2.2px solid var(--on-accent);
		transform: rotate(-45deg);
	}
	.pick:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.row {
		grid-column: 2;
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
		min-width: 0;
		min-height: 44px;
		padding: var(--space-2-5) var(--space-1);
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
		-webkit-touch-callout: none;
	}
	.lines {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1;
		min-width: 0;
	}
	.top {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: start;
		gap: 0 var(--space-2);
		min-width: 0;
	}
	.from {
		grid-column: 1;
		grid-row: 1 / span 2;
		min-width: 0;
		font-weight: 600;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.when {
		grid-column: 2;
		grid-row: 1;
		display: inline-flex;
		align-items: center;
		gap: 2px;
		justify-self: end;
		color: var(--muted);
	}
	.marks {
		grid-column: 2;
		grid-row: 2;
		justify-self: end;
		display: inline-flex;
		gap: var(--space-1);
		color: var(--muted);
	}
	.mark {
		display: inline-flex;
	}
	.mark.flagged {
		color: var(--status-warn);
	}
	time {
		flex: none;
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: nowrap;
	}
	.subject {
		overflow-wrap: break-word;
		hyphens: auto;
		color: var(--text);
	}
	.subject.large {
		display: inline-flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-style: italic;
	}
	.preview {
		color: var(--muted);
		font-size: var(--text-md);
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.where {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.unread .subject {
		font-weight: 700;
	}
	.dot {
		position: absolute;
		left: 5px;
		top: calc(var(--space-2-5) + 7px);
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: var(--accent);
	}
	.count {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		min-height: 32px;
		padding: 0 var(--space-1-5);
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.count[aria-expanded='true'] {
		border-color: var(--accent);
	}
	/* The quick actions show on hover and focus, over the end of the first line. */
	.quick {
		position: absolute;
		top: var(--space-1);
		right: var(--space-2);
		display: flex;
		gap: 2px;
		padding: 2px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--surface);
		box-shadow: var(--shadow-menu);
		opacity: 0;
		pointer-events: none;
	}
	.entry:hover .quick,
	.entry:focus-within .quick {
		opacity: 1;
		pointer-events: auto;
	}
	.qa {
		display: inline-grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.qa:hover,
	.qa:focus-visible {
		background: var(--chip);
	}
	.qa[aria-pressed='true'] {
		color: var(--status-warn);
	}
	@media (max-width: 899px) {
		.row {
			min-height: 56px;
		}
		.pick {
			width: 22px;
			height: 22px;
		}
		.count {
			min-height: 44px;
			min-width: 44px;
		}
	}
</style>
