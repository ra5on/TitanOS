<script lang="ts">
	// One message: its head (To/Cc open on request), the actions, the bar for
	// blocked external pictures, the text or the HTML in the sandboxed frame,
	// the real targets of its links and the attachments with "Herunterladen"
	// and "In Dateien speichern".
	import { untrack } from 'svelte';
	import BottomBar from '../components/BottomBar.svelte';
	import Dialog from '../components/Dialog.svelte';
	import PhoneDockHandle from '../desktop/PhoneDockHandle.svelte';
	import { immersivePhone } from '../desktop/immersive-phone.svelte.ts';
	import SenderAvatar from './SenderAvatar.svelte';
	import Dropdown from '../components/Dropdown.svelte';
	import Icon from '../components/Icon.svelte';
	import DocsFolderPicker from '../components/agent/features/DocsFolderPicker.svelte';
	import { place } from '../features/docs.ts';
	import { formatBytes } from '../format.ts';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import Confirm from '../nvr/Confirm.svelte';
	import { mailApi } from './api.ts';
	import type { MailLink } from './html.ts';
	import MailFrame from './MailFrame.svelte';
	import { findFolder, folderName, mail, sortFolders } from './store.svelte.ts';
	import { addressText, fullDate, linkify, senderName } from './text.ts';
	import type { Attachment, ComposeMode, Flags, Message } from './types.ts';

	let {
		accountId,
		folder,
		uid,
		compact = false,
		readerBack,
		oncompose,
		onflags,
		onremoved,
		onagent,
	}: {
		accountId: string;
		folder: string;
		uid: number;
		/**
		 * 1.13: the phone and windows under 620 px – the actions as a bar at
		 * the bottom (BottomBar), answering and the rest in menus.
		 */
		compact?: boolean;
		readerBack?: { label: string; href: string };
		oncompose: (mode: ComposeMode, msg: Message) => void;
		onflags: (uids: number[], change: Partial<Flags>) => void;
		/** The message left this folder (moved, deleted). */
		onremoved: (uid: number) => void;
		/** 1.13: opens the agent's panel with this message (only with the agent). */
		onagent?: () => void;
	} = $props();

	let msg = $state<Message | null>(null);
	let error = $state('');
	let images = $state(false);
	let showAll = $state(false);
	let links = $state<MailLink[]>([]);
	let busy = $state(false);
	let actionError = $state('');
	let notice = $state('');
	let moving = $state(false);
	let confirmDelete = $state(false);
	let saving = $state<Attachment | null>(null);
	let asked = 0;

	async function load() {
		const n = ++asked;
		error = '';
		try {
			const got = await mailApi.message(accountId, folder, uid, { images });
			if (n !== asked) return;
			msg = got;
			// Opening marks it read (titand does it with the request); only a
			// change is passed on (it reads the folders again).
			if (!got.flags.seen) {
				got.flags.seen = true;
				onflags([uid], { seen: true });
			}
		} catch (err) {
			if (n === asked) error = apiErrorText(err);
		}
	}

	// Another message starts without pictures, with its head closed.
	$effect(() => {
		void accountId;
		void folder;
		void uid;
		untrack(() => {
			msg = null;
			images = false;
			showAll = false;
			links = [];
			actionError = '';
			notice = '';
			void load();
		});
	});

	function loadImages() {
		images = true;
		void load();
	}

	const inTrash = $derived(findFolder(accountId, folder)?.role === 'trash');
	const inArchive = $derived(findFolder(accountId, folder)?.role === 'archive');
	const recipients = $derived.by(() => {
		if (!msg) return '';
		const all = [...msg.to, ...msg.cc];
		const names = all.slice(0, 2).map(senderName).join(', ');
		return all.length > 2 ? t('mail.head.andMore', { names, count: all.length - 2 }) : names;
	});
	const files = $derived(
		(msg?.attachments ?? []).filter((a) => !a.inline || !/^image\//.test(a.type)),
	);

	async function act(run: () => Promise<unknown>) {
		busy = true;
		actionError = '';
		notice = '';
		try {
			await run();
			return true;
		} catch (err) {
			actionError = apiErrorText(err, { detail: true });
			return false;
		} finally {
			busy = false;
		}
	}

	async function setFlag(flag: keyof Flags, on: boolean) {
		if (!msg) return;
		const ok = await act(() =>
			mailApi.flags(accountId, {
				folder,
				uids: [uid],
				add: on ? [flag] : [],
				remove: on ? [] : [flag],
			}),
		);
		if (ok && msg) {
			msg.flags[flag] = on;
			onflags([uid], { [flag]: on });
		}
	}

	async function moveTo(to: string) {
		moving = false;
		if (await act(() => mailApi.move(accountId, { folder, uids: [uid], to }))) onremoved(uid);
	}

	/** 1.13 (the bar at the bottom): into the mailbox's archive, like the list's action. */
	async function archive() {
		if (await act(() => mailApi.archive(accountId, { folder, uids: [uid] }))) onremoved(uid);
	}

	async function remove() {
		confirmDelete = false;
		if (await act(() => mailApi.remove(accountId, { folder, uids: [uid] }))) onremoved(uid);
	}
	function askDelete() {
		// In the trash it is gone for good: asked first. Elsewhere it goes to the trash.
		if (inTrash) confirmDelete = true;
		else void remove();
	}

	async function saveTo(target: { root: string; path: string }) {
		const part = saving;
		saving = null;
		if (!part) return;
		const where = place(target);
		const name = (part.name || 'attachment').replace(/[/\\]/g, '_');
		const ok = await act(() =>
			mailApi.savePart(accountId, {
				folder,
				uid,
				part: part.part,
				root: target.root,
				path: target.path ? `${target.path}/${name}` : name,
			}),
		);
		if (ok) notice = t('mail.attachment.saved', { name: part.name, place: where });
	}

	const moveTargets = $derived(
		sortFolders(mail.folders[accountId] ?? []).filter((f) => f.path !== folder),
	);
	function filingActions(m: Message) {
		return [
			{ label: t('mail.action.move'), run: () => (moving = true) },
			...(!inArchive ? [{ label: t('mail.action.archive'), run: archive }] : []),
			{
				label: t(m.flags.flagged ? 'mail.action.unflag' : 'mail.action.flag'),
				run: () => setFlag('flagged', !m.flags.flagged),
			},
			{
				label: t(m.flags.seen ? 'mail.action.markUnread' : 'mail.action.markRead'),
				run: () => setFlag('seen', !m.flags.seen),
			},
			...(onagent ? [{ label: t('agentpim.panel.title'), run: onagent }] : []),
		];
	}
	function readerActions(m: Message) {
		return [
			{ label: t('mail.action.replyAll'), run: () => oncompose('replyAll', m) },
			{ label: t('mail.action.forward'), run: () => oncompose('forward', m) },
			...filingActions(m),
			{ label: t(inTrash ? 'mail.action.deleteForever' : 'mail.action.delete'), run: askDelete },
		];
	}
</script>

{#if error}
	<p class="notice-error pad" role="alert">{error}</p>
{:else if !msg}
	<p class="muted pad" role="status">{t('common.loading')}</p>
{:else}
	<article class="message" class:reader={!!readerBack} aria-labelledby="mail-message-subject">
		{#if readerBack}
			{@const m = msg}
			<nav
				class="reader-nav"
				aria-label={t('mail.actions')}
				data-nordlicht-immersive="mail"
				use:immersivePhone
			>
				<a
					class="reader-back"
					href={readerBack.href}
					aria-label={t('mail.back.list', { folder: readerBack.label })}
				>
					<Icon name="chevronLeft" size={21} /><span>{readerBack.label}</span>
				</a>
				<Dropdown label={t('ui.more')} icon="more" align="right" disabled={busy}>
					{#snippet children(close)}
						{#each readerActions(m) as action (action.label)}
							<button
								type="button"
								role="menuitem"
								onclick={() => {
									close();
									action.run();
								}}>{action.label}</button
							>
						{/each}
					{/snippet}
				</Dropdown>
			</nav>
		{/if}
		<header class="head">
			<h2 id="mail-message-subject" tabindex="-1">{msg.subject.trim() || t('mail.noSubject')}</h2>
			{#if readerBack}
				<button
					type="button"
					class="sender-details"
					aria-expanded={showAll}
					aria-controls="mail-recipients"
					onclick={() => (showAll = !showAll)}
				>
					<SenderAvatar from={msg.from} size={32} />
					<span
						><strong>{senderName(msg.from)}</strong><time datetime={msg.date}
							>{fullDate(msg.date, i18n.locale)}</time
						></span
					>
					<Icon name={showAll ? 'chevronUp' : 'chevronDown'} size={16} />
					<span class="sr-only">{t(showAll ? 'mail.head.less' : 'mail.head.more')}</span>
				</button>
			{:else}
				<p class="from">
					<span class="label">{t('mail.head.from')}</span>
					<span class="who">{addressText(msg.from)}</span>
				</p>
				<p class="date"><time datetime={msg.date}>{fullDate(msg.date, i18n.locale)}</time></p>
				<div class="to">
					<span class="label">{t('mail.head.to')}</span>
					<span class="who">{recipients}</span>
					<button
						type="button"
						class="btn toggle"
						aria-expanded={showAll}
						aria-controls="mail-recipients"
						onclick={() => (showAll = !showAll)}
						>{showAll ? t('mail.head.less') : t('mail.head.more')}</button
					>
				</div>
			{/if}
			{#if showAll}
				<dl id="mail-recipients" class="all">
					{#if readerBack}
						<dt>{t('mail.head.from')}</dt>
						<dd>{addressText(msg.from)}</dd>
					{/if}
					<dt>{t('mail.head.to')}</dt>
					<dd>{msg.to.map(addressText).join(', ') || '–'}</dd>
					{#if msg.cc.length}
						<dt>{t('mail.head.cc')}</dt>
						<dd>{msg.cc.map(addressText).join(', ')}</dd>
					{/if}
					{#if msg.bcc?.length}
						<dt>{t('mail.head.bcc')}</dt>
						<dd>{msg.bcc.map(addressText).join(', ')}</dd>
					{/if}
					{#if msg.replyTo.length}
						<dt>{t('mail.head.replyTo')}</dt>
						<dd>{msg.replyTo.map(addressText).join(', ')}</dd>
					{/if}
				</dl>
			{/if}
		</header>

		{#if !compact}<div class="actions" role="toolbar" aria-label={t('mail.actions')}>
				<button type="button" class="btn" disabled={busy} onclick={() => oncompose('reply', msg!)}
					><Icon name="reply" size={16} />{t('mail.action.reply')}</button
				>
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => oncompose('replyAll', msg!)}
					><Icon name="replyAll" size={16} />{t('mail.action.replyAll')}</button
				>
				<button type="button" class="btn" disabled={busy} onclick={() => oncompose('forward', msg!)}
					><Icon name="forward" size={16} />{t('mail.action.forward')}</button
				>
				<button type="button" class="btn" disabled={busy} onclick={askDelete}
					><Icon name="trash" size={16} />{inTrash
						? t('mail.action.deleteForever')
						: t('mail.action.delete')}</button
				>
				<Dropdown label={t('ui.more')} icon="more" align="right" disabled={busy}>
					{#snippet children(close)}
						{#each filingActions(msg!) as action (action.label)}
							<button
								type="button"
								role="menuitem"
								onclick={() => {
									close();
									action.run();
								}}>{action.label}</button
							>
						{/each}
					{/snippet}
				</Dropdown>
			</div>{/if}

		{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}

		{#if msg.externalImages && !images}
			<div class="images" role="note">
				<Icon name="image" size={16} />
				<span>{t('mail.images.blocked')}</span>
				<button type="button" class="btn" onclick={loadImages}>{t('mail.images.load')}</button>
			</div>
		{/if}

		{#if msg.rawText}
			<p class="notice-warn" role="note" data-raw-text>{t('mail.rawText')}</p>
		{:else if msg.truncated}
			<p class="notice-warn" role="note">{t('mail.truncated')}</p>
		{/if}

		{#snippet attachments()}
			{#if files.length}
				<section class="files" aria-label={t('mail.attachment.title')}>
					<h3>{t('mail.attachment.title')}</h3>
					<ul>
						{#each files as a (a.part)}
							<li>
								<Icon name="clip" size={16} />
								<span class="fname">{a.name || t('mail.attachment.noName')}</span>
								<span class="muted fsize">{formatBytes(a.size)}</span>
								<span class="fbtns">
									<a
										class="btn"
										href={mailApi.partUrl(accountId, folder, uid, a.part)}
										download={a.name || 'attachment'}
										><Icon name="download" size={16} />{t('mail.attachment.download')}</a
									>
									<button type="button" class="btn" disabled={busy} onclick={() => (saving = a)}
										><Icon name="folder" size={16} />{t('mail.attachment.save')}</button
									>
								</span>
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		{/snippet}
		{#if !readerBack}{@render attachments()}{/if}

		<div class="body">
			{#if msg.html}
				<MailFrame
					html={msg.html}
					title={t('mail.frameTitle', { subject: msg.subject || t('mail.noSubject') })}
					onlinks={(l) => (links = l)}
				/>
			{:else}
				<div class="text">
					{#each linkify(msg.text) as piece, i (i)}{#if piece.href}<a
								href={piece.href}
								target="_blank"
								rel="noopener noreferrer"
								title={piece.href}>{piece.text}</a
							>{:else}{piece.text}{/if}{/each}
				</div>
			{/if}
		</div>

		{#if links.length}
			<details class="links">
				<summary
					>{t('mail.link.title', { count: links.length })}{#if links.some((l) => l.warn)}
						· <strong>{t('mail.link.someWarn')}</strong>{/if}</summary
				>
				<ul>
					{#each links as l, i (i)}
						<li>
							<span class="ltext">{l.text}</span>
							<span class="lhref"
								>→ <a href={l.href} target="_blank" rel="noopener noreferrer">{l.href}</a></span
							>
							{#if l.warn}<span class="lwarn"
									><Icon name="info" size={14} />{t('mail.link.warn', { host: l.warn })}</span
								>{/if}
						</li>
					{/each}
				</ul>
			</details>
		{/if}
		{#if readerBack}
			{@render attachments()}
			<div class="reader-foot" role="toolbar" aria-label={t('mail.actions')}>
				<button
					type="button"
					class="reader-reply"
					disabled={busy}
					onclick={() => oncompose('reply', msg!)}
					><Icon name="reply" size={19} />{t('mail.action.reply')}</button
				>
				<PhoneDockHandle />
			</div>
		{:else if compact}
			{@const m = msg}
			<BottomBar label={t('mail.actions')}>
				<Dropdown label={t('mail.action.reply')} icon="reply" disabled={busy}>
					{#snippet children(close)}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								oncompose('reply', m);
							}}>{t('mail.action.reply')}</button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								oncompose('replyAll', m);
							}}>{t('mail.action.replyAll')}</button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								oncompose('forward', m);
							}}>{t('mail.action.forward')}</button
						>
					{/snippet}
				</Dropdown>
				<button
					type="button"
					class="bar-btn"
					disabled={busy}
					aria-label={t('mail.action.move')}
					title={t('mail.action.move')}
					onclick={() => (moving = true)}><Icon name="move" size={20} /></button
				>
				{#if !inArchive}
					<button
						type="button"
						class="bar-btn"
						disabled={busy}
						aria-label={t('mail.action.archive')}
						title={t('mail.action.archive')}
						onclick={archive}><Icon name="archive" size={20} /></button
					>
				{/if}
				<button
					type="button"
					class="bar-btn"
					disabled={busy}
					aria-label={inTrash ? t('mail.action.deleteForever') : t('mail.action.delete')}
					title={inTrash ? t('mail.action.deleteForever') : t('mail.action.delete')}
					onclick={askDelete}><Icon name="trash" size={20} /></button
				>
				<button
					type="button"
					class="bar-btn"
					disabled={busy}
					aria-pressed={m.flags.flagged}
					aria-label={t('mail.action.flag')}
					title={m.flags.flagged ? t('mail.action.unflag') : t('mail.action.flag')}
					onclick={() => setFlag('flagged', !m.flags.flagged)}
					><Icon name="flag" size={20} /></button
				>
				<Dropdown label={t('ui.more')} icon="more" align="right" disabled={busy}>
					{#snippet children(close)}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								void setFlag('seen', !m.flags.seen);
							}}>{m.flags.seen ? t('mail.action.markUnread') : t('mail.action.markRead')}</button
						>
						{#if onagent}
							<button
								type="button"
								role="menuitem"
								onclick={() => {
									close();
									onagent();
								}}>{t('agentpim.panel.title')}</button
							>
						{/if}
					{/snippet}
				</Dropdown>
			</BottomBar>
		{/if}
	</article>
{/if}

{#if moving}
	<Dialog title={t('mail.move.title')} onclose={() => (moving = false)}>
		<ul class="targets">
			{#each moveTargets as f (f.path)}
				<li>
					<button type="button" class="btn target" onclick={() => moveTo(f.path)}
						>{folderName(f)}</button
					>
				</li>
			{:else}
				<li class="muted">{t('mail.move.none')}</li>
			{/each}
		</ul>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (moving = false)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirmDelete && msg}
	<Confirm
		title={t('mail.delete.title')}
		text={[
			t('mail.delete.text', { subject: msg.subject || t('mail.noSubject') }),
			t('mail.delete.forever'),
		]}
		confirm={t('mail.action.deleteForever')}
		danger
		{busy}
		onconfirm={remove}
		onclose={() => (confirmDelete = false)}
	/>
{/if}

{#if saving}
	<DocsFolderPicker
		title={t('mail.attachment.saveTitle', { name: saving.name })}
		writable
		onpick={saveTo}
		onclose={() => (saving = null)}
	/>
{/if}

<style>
	.pad {
		margin: var(--space-4);
	}
	.message {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
		min-height: 100%;
		padding: var(--space-3) var(--space-4);
	}
	.head {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	h2 {
		margin: 0 0 var(--space-1);
		font-size: var(--text-xl);
		line-height: 1.25;
		overflow-wrap: anywhere;
	}
	h2:focus {
		outline: none;
	}
	.head p {
		margin: 0;
	}
	.from,
	.to {
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--space-2);
		align-items: baseline;
	}
	.label {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.who {
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.date {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.toggle {
		min-height: 32px;
	}
	.all {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: var(--space-1) var(--space-3);
		margin: var(--space-1) 0 0;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
	}
	.all dt {
		color: var(--muted);
	}
	.all dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.actions .btn,
	.fbtns .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	:global([data-nord-desktop]) .message:not(.reader) .text {
		max-inline-size: 75ch;
		line-height: 1.55;
	}
	.images {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-small);
		background: var(--warn-bg);
		color: var(--warn-text);
	}
	.images span {
		flex: 1 1 12rem;
		overflow-wrap: break-word;
	}
	.files h3 {
		margin: 0 0 var(--space-1-5);
		font-size: var(--text-md);
	}
	.files ul,
	.links ul,
	.targets {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.files li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5) var(--space-2);
		padding: var(--space-1-5) 0;
		border-top: 1px solid var(--border);
	}
	.fname {
		flex: 1 1 10rem;
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.fsize {
		font-size: var(--text-sm);
	}
	.fbtns {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.body {
		display: flex;
		flex-direction: column;
		flex: 1 1 auto;
		min-height: 0;
	}
	.text {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-md);
		line-height: 1.5;
	}
	.links summary {
		cursor: pointer;
		min-height: 32px;
		overflow-wrap: break-word;
	}
	.links li {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-1-5) 0;
		border-top: 1px solid var(--border);
	}
	.ltext,
	.lhref {
		overflow-wrap: anywhere;
	}
	.lhref {
		font-size: var(--text-sm);
	}
	.lwarn {
		display: inline-flex;
		gap: var(--space-1);
		align-items: flex-start;
		color: var(--warn-text);
		font-weight: 600;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.target {
		width: 100%;
		margin-bottom: var(--space-1);
		text-align: left;
		overflow-wrap: anywhere;
	}
	@media (max-width: 899px) {
		.message {
			padding: var(--space-3);
		}
		.actions .btn,
		.fbtns .btn,
		.images .btn,
		.target,
		.toggle,
		.links summary {
			min-height: 44px;
		}
	}

	.message.reader {
		padding: 0 max(18px, env(safe-area-inset-right)) 0 max(18px, env(safe-area-inset-left));
		gap: 12px;
		min-height: calc(100dvh - env(safe-area-inset-top));
		background: color-mix(in srgb, var(--surface) 82%, transparent);
	}
	.reader-nav,
	.reader-foot {
		position: sticky;
		z-index: 5;
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-inline: -10px;
		background: color-mix(in srgb, var(--surface) 94%, transparent);
	}
	.reader-nav {
		top: 0;
		border-bottom: 1px solid var(--border);
	}
	.reader-back,
	.reader-reply {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		min-height: 48px;
		padding: 4px 9px;
		border: 0;
		border-radius: var(--radius-small);
		background: transparent;
		color: var(--accent);
		text-decoration: none;
		font: inherit;
		font-weight: 650;
		font-size: 17px;
		cursor: pointer;
	}
	.reader-back {
		min-width: min(125px, 70%);
		max-width: calc(100% - 48px);
		overflow-wrap: anywhere;
	}
	.reader-back :global(svg) {
		flex: none;
	}
	.reader-nav :global(.dropdown > .btn) {
		min-width: 44px;
		min-height: 44px;
		border: 0;
		background: transparent;
	}
	.reader .head {
		gap: 5px;
		border-bottom: 1px solid var(--border);
		padding-bottom: 6px;
	}
	.reader h2 {
		font-size: 24px;
		letter-spacing: -0.025em;
		line-height: 1.16;
	}
	.sender-details {
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 2px 0;
		min-height: 44px;
		border: 0;
		color: var(--text);
		background: transparent;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.sender-details > span:not(.sr-only) {
		display: grid;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.sender-details time {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.reader-foot {
		bottom: 0;
		padding: 0 8px env(safe-area-inset-bottom);
		border-top: 1px solid var(--border);
	}
	.reader .text {
		font-size: max(var(--text-md), 16px);
		line-height: 1.55;
	}
	@supports ((backdrop-filter: blur(12px)) or (-webkit-backdrop-filter: blur(12px))) {
		.reader-nav,
		.reader-foot {
			background: color-mix(in srgb, var(--surface) 85%, transparent);
			-webkit-backdrop-filter: blur(14px);
			backdrop-filter: blur(14px);
		}
	}
</style>
