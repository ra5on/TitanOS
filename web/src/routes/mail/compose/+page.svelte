<script lang="ts">
	// 1.05, T-Mail: writing an e-mail (docs/TMAIL.md, "Schreiben"). From one
	// of the account's mailboxes (the sender is always its address), To/Cc/Bcc
	// with suggestions, subject, plain text, a signature to choose, files
	// uploaded or taken from the NAS. The draft saves itself every 30 s while
	// it changes. On the desktop this page is a window of its own; sending
	// closes it, throwing it away asks first.
	import { onDestroy, onMount } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api.ts';
	import NasPicker from '#lib/components/agent/NasPicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import AddressField from '#lib/tmail/AddressField.svelte';
	import { mailApi, nasFileOf, sendForm } from '#lib/tmail/api.ts';
	import { createAutosave } from '#lib/tmail/autosave.ts';
	import { closeCompose } from '#lib/tmail/open.ts';
	import { folderOf, loadFolders, mail } from '#lib/tmail/store.svelte.ts';
	import {
		addrOf,
		addressText,
		forwardSubject,
		fullDate,
		quote,
		replyRecipients,
		replySubject,
		senderName,
		validAddress,
	} from '#lib/tmail/text.ts';
	import type { Attachment, Message, SendMeta, Signature } from '#lib/tmail/types.ts';
	import { draftList, loadAgentDraft } from '#lib/agent/drafts.ts';

	/** Contract: at most 100 recipients and 25 MB of attachments. */
	const MAX_RECIPIENTS = 100;
	const MAX_BYTES = 25 * 1000 * 1000;

	const params = page.url.searchParams;
	const mode = params.get('mode') ?? '';
	const sourceFolder = params.get('f') ?? '';
	const sourceUid = Number(params.get('uid')) || 0;
	const back = (() => {
		const b = params.get('back') ?? '';
		return b.startsWith('/mail') && !b.startsWith('//') ? b : '/mail';
	})();

	let accountId = $state(params.get('a') || mail.accounts?.[0]?.id || '');
	let to = $state<string[]>(params.get('to') ? [params.get('to')!] : []);
	let cc = $state<string[]>([]);
	let bcc = $state<string[]>([]);
	let showCopies = $state(false);
	let subject = $state('');
	let text = $state('');
	let signature = $state('');
	let signatures = $state<Signature[]>([]);
	let files = $state<File[]>([]);
	let nasFiles = $state<string[]>([]);
	// Attachments of an existing mail taken along: of the forwarded mail
	// (folder, uid), or of the draft `draftUid` names (draft: true) – a draft
	// opened again, and after every save everything attached, so that files
	// go up only once (attachFrom without uid, docs/TMAIL.md).
	let carried = $state<{
		folder?: string;
		uid?: number;
		parts: Attachment[];
		draft: boolean;
	} | null>(null);
	let inReplyTo = '';
	let references = '';
	let draftUid = $state(0);

	// 1.13: a draft of the KI-Agent (?agentDraft=<chat>:<entry>, "Öffnen und
	// bearbeiten"): its fields come in as text to check; nothing is sent
	// without the person.
	const agentDraft = params.get('agentDraft');
	let fromAgent = $state(false);
	let agentAnswers: { folder: string; uid: number } | null = null;

	let loading = $state((!!mode && !!sourceUid) || !!agentDraft);
	let loadError = $state('');
	let sending = $state(false);
	let sendError = $state('');
	let draftNote = $state('');
	let draftError = $state('');
	let pickingNas = $state(false);
	let confirmDiscard = $state(false);
	let discarding = $state(false);
	let done = false;
	// Changes since the last saved draft (for the window's question when closing).
	let edits = $state(0);
	let savedEdits = $state(0);
	const unsaved = $derived(edits !== savedEdits);

	const account = $derived(mail.accounts?.find((a) => a.id === accountId));
	const chosenSignature = $derived(signatures.find((s) => s.id === signature));
	const bytes = $derived(
		files.reduce((sum, f) => sum + f.size, 0) +
			(carried?.parts ?? []).reduce((sum, a) => sum + (a.size || 0), 0),
	);
	const recipients = $derived([...to, ...cc, ...bcc]);

	function touch() {
		edits++;
		autosave.touch();
	}

	function meta(): SendMeta {
		const m: SendMeta = {
			to: to.map((v) => v.trim()),
			cc: cc.map((v) => v.trim()),
			bcc: bcc.map((v) => v.trim()),
			subject,
			text,
			inReplyTo,
			references,
			signature,
			draftUid,
		};
		if (carried?.parts.length)
			m.attachFrom = carried.draft
				? { parts: carried.parts.map((a) => a.part) }
				: { folder: carried.folder, uid: carried.uid, parts: carried.parts.map((a) => a.part) };
		// A reply marks the mail it answers once sent.
		if (sourceUid && (mode === 'reply' || mode === 'replyAll'))
			m.answers = { folder: sourceFolder, uid: sourceUid };
		// 1.12: a forward marks the mail it forwards ($Forwarded).
		if (sourceUid && mode === 'forward') m.forwards = { folder: sourceFolder, uid: sourceUid };
		if (agentAnswers) m.answers = agentAnswers;
		return m;
	}

	async function saveDraft() {
		if (!accountId || done) return;
		const at = edits;
		draftError = '';
		const sentFiles = [...files];
		const sentNas = [...nasFiles];
		try {
			const res = await mailApi.draft(
				accountId,
				sendForm(meta(), sentFiles, sentNas.map(nasFileOf)),
			);
			if (res && typeof res.uid === 'number' && res.uid > 0) {
				draftUid = res.uid;
				// Everything attached is in the new draft now: the next save takes
				// it from there; only files added meanwhile still go up.
				if (Array.isArray(res.attachments)) {
					carried = { parts: res.attachments.filter((a) => !a.inline), draft: true };
					files = files.filter((f) => !sentFiles.includes(f));
					nasFiles = nasFiles.filter((r) => !sentNas.includes(r));
				}
			}
			savedEdits = at;
			draftNote = t('mail.compose.draftSaved', {
				time: new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(
					new Date(),
				),
			});
		} catch (err) {
			draftError = t('mail.compose.draftFailed', { reason: apiErrorText(err) });
			throw err;
		}
	}
	const autosave = createAutosave(saveDraft);
	onDestroy(() => autosave.stop());

	guardWindowClose(() => (sending ? 'busy' : unsaved && !done ? 'edits' : ''));

	// Leaving the page on the phone (arrow, swipe) keeps what was written as a draft.
	beforeNavigate((nav) => {
		if (done || !unsaved || nav.willUnload || !nav.to) return;
		nav.cancel();
		const target = nav.to.url.pathname + nav.to.url.search;
		void autosave.flush().finally(() => {
			done = true;
			void goto(target);
		});
	});

	function startFrom(msg: Message) {
		const me = account?.email ?? '';
		if (mode === 'reply' || mode === 'replyAll') {
			const r = replyRecipients(msg, me, mode === 'replyAll');
			to = r.to;
			cc = r.cc;
			showCopies = cc.length > 0;
			subject = replySubject(msg.subject);
			inReplyTo = msg.messageId;
			references = `${msg.references ?? ''} ${msg.messageId}`.trim();
			const head = t('mail.compose.wrote', {
				date: fullDate(msg.date, i18n.locale),
				name: senderName(msg.from),
			});
			text = `\n\n${head}\n${quote(msg.text)}\n`;
		} else if (mode === 'forward') {
			subject = forwardSubject(msg.subject);
			carried = {
				folder: sourceFolder,
				uid: sourceUid,
				parts: msg.attachments.filter((a) => !a.inline),
				draft: false,
			};
			const lines = [
				t('mail.compose.forwardHead'),
				`${t('mail.head.from')} ${addressText(msg.from)}`,
				`${t('mail.compose.date')} ${fullDate(msg.date, i18n.locale)}`,
				`${t('mail.compose.subject')}: ${msg.subject}`,
				`${t('mail.head.to')} ${msg.to.map(addressText).join(', ')}`,
			];
			if (msg.cc.length) lines.push(`${t('mail.head.cc')} ${msg.cc.map(addressText).join(', ')}`);
			text = `\n\n${lines.join('\n')}\n\n${msg.text}\n`;
		} else if (mode === 'draft') {
			to = msg.to.map(addressText);
			cc = msg.cc.map(addressText);
			bcc = (msg.bcc ?? []).map(addressText);
			showCopies = cc.length > 0 || bcc.length > 0;
			subject = msg.subject;
			text = msg.text;
			draftUid = msg.uid;
			inReplyTo = msg.inReplyTo;
			references = msg.references ?? '';
			carried = { parts: msg.attachments.filter((a) => !a.inline), draft: true };
		}
	}

	async function startFromAgent(param: string) {
		const d = await loadAgentDraft(param);
		if (!d || d.app !== 'mail' || d.kind === 'move') {
			loadError = t('agentpim.draft.gone');
			return;
		}
		const f = d.fields;
		if (typeof f.a === 'string' && mail.accounts?.some((a) => a.id === f.a)) accountId = f.a;
		to = draftList(f.to);
		cc = draftList(f.cc);
		bcc = draftList(f.bcc);
		showCopies = cc.length > 0 || bcc.length > 0;
		subject = typeof f.subject === 'string' ? f.subject : '';
		text = typeof f.text === 'string' ? f.text : '';
		// An answer: the mail it answers is marked once sent, and the thread kept.
		if (d.kind === 'reply' && typeof f.f === 'string' && typeof f.uid === 'number') {
			agentAnswers = { folder: f.f, uid: f.uid };
			try {
				const orig = await mailApi.message(accountId, f.f, f.uid);
				inReplyTo = orig.messageId;
				references = `${orig.references ?? ''} ${orig.messageId}`.trim();
			} catch {
				// Sent without the thread's references; the answer still goes.
			}
		}
		fromAgent = true;
		touch();
	}

	onMount(async () => {
		if (accountId && !mail.folders[accountId]) void loadFolders(accountId);
		mailApi.settings().then(
			(s) => (signatures = s.signatures ?? []),
			() => (signatures = []),
		);
		if (!loading) return;
		try {
			if (agentDraft) await startFromAgent(agentDraft);
			else startFrom(await mailApi.message(accountId, sourceFolder, sourceUid));
		} catch (err) {
			loadError = apiErrorText(err);
		} finally {
			loading = false;
		}
	});

	// The mailbox's own signature, until one is chosen.
	let signatureChosen = false;
	$effect(() => {
		const own = account?.signature ?? '';
		if (!signatureChosen && signatures.length) signature = own;
	});

	function addFiles(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const chosen = [...(input.files ?? [])];
		input.value = '';
		if (!chosen.length) return;
		files = [
			...files,
			...chosen.filter(
				(f) =>
					!files.some(
						(g) => g.name === f.name && g.size === f.size && g.lastModified === f.lastModified,
					),
			),
		];
		touch();
	}
	function removeFile(f: File) {
		files = files.filter((g) => g !== f);
		touch();
	}
	function addNas(refs: string[]) {
		pickingNas = false;
		nasFiles = [...new Set([...nasFiles, ...refs])];
		touch();
	}
	function removeCarried(a: Attachment) {
		if (carried) carried = { ...carried, parts: carried.parts.filter((p) => p !== a) };
		touch();
	}
	function removeNas(ref: string) {
		nasFiles = nasFiles.filter((r) => r !== ref);
		touch();
	}
	const nasName = (ref: string) => {
		const path = ref.slice(ref.indexOf(':') + 1);
		return path.split('/').at(-1) || path;
	};

	/** What stops sending, in words; '' when it may go. */
	function problem(): string {
		if (!account) return t('mail.compose.noAccount');
		if (!to.length && !cc.length && !bcc.length) return t('mail.compose.noRecipient');
		const bad = recipients.filter((r) => !validAddress(addrOf(r)));
		if (bad.length) return t('mail.compose.badRecipients', { list: bad.join(', ') });
		if (recipients.length > MAX_RECIPIENTS)
			return t('mail.compose.tooMany', { max: MAX_RECIPIENTS });
		if (bytes > MAX_BYTES)
			return t('mail.compose.tooLarge', { size: formatBytes(bytes), max: formatBytes(MAX_BYTES) });
		return '';
	}

	async function send() {
		sendError = problem();
		if (sendError) return;
		sending = true;
		try {
			await mailApi.send(accountId, sendForm(meta(), files, nasFiles.map(nasFileOf)));
			done = true;
			autosave.stop();
			savedEdits = edits;
			closeCompose(back);
		} catch (err) {
			// mail_send_failed carries the server's answer (without secrets): shown below the text.
			sendError =
				err instanceof ApiError && err.code === 'mail_send_failed'
					? apiErrorText(err, { detail: true })
					: apiErrorText(err);
		} finally {
			sending = false;
		}
	}

	async function discard() {
		discarding = true;
		try {
			const drafts = folderOf(accountId, 'drafts');
			if (draftUid && drafts)
				await mailApi.remove(accountId, { folder: drafts.path, uids: [draftUid] });
		} catch {
			// The draft stays in "Entwürfe"; nothing else is lost.
		}
		done = true;
		autosave.stop();
		savedEdits = edits;
		confirmDiscard = false;
		discarding = false;
		closeCompose(back);
	}
	function askDiscard() {
		if (!unsaved && !draftUid) {
			done = true;
			closeCompose(back);
			return;
		}
		confirmDiscard = true;
	}

	const title = $derived(subject.trim() || t('mail.compose.title'));
</script>

<PageHead {title} path={[{ label: t('desktop.app.mail'), href: back }]}>
	{#snippet actions()}
		<button type="button" class="btn" disabled={sending} onclick={askDiscard}
			><Icon name="trash" size={16} />{t('mail.compose.discard')}</button
		>
		<button
			type="button"
			class="btn btn-primary"
			disabled={sending || loading || !account}
			onclick={send}><Icon name="send" size={16} />{t('mail.compose.send')}</button
		>
	{/snippet}
</PageHead>

{#if loadError}<p class="notice-error" role="alert">{loadError}</p>{/if}
{#if fromAgent}<p class="notice-warn" role="note">{t('agentpim.draft.composeNote')}</p>{/if}
{#if loading}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<form class="compose" onsubmit={(e) => e.preventDefault()}>
		<div class="row">
			<label for="mail-from" class="lbl">{t('mail.compose.from')}</label>
			<select
				id="mail-from"
				class="field"
				bind:value={accountId}
				onchange={() => {
					signatureChosen = false;
					if (!mail.folders[accountId]) void loadFolders(accountId);
					touch();
				}}
			>
				{#each mail.accounts ?? [] as a (a.id)}
					<option value={a.id}>{a.name ? `${a.name} <${a.email}>` : a.email}</option>
				{/each}
			</select>
		</div>
		<AddressField id="mail-to" label={t('mail.head.to')} bind:values={to} onchange={touch} />
		{#if showCopies}
			<AddressField id="mail-cc" label={t('mail.head.cc')} bind:values={cc} onchange={touch} />
			<AddressField id="mail-bcc" label={t('mail.head.bcc')} bind:values={bcc} onchange={touch} />
		{:else}
			<div class="row">
				<span></span>
				<button type="button" class="btn copies" onclick={() => (showCopies = true)}
					>{t('mail.compose.showCopies')}</button
				>
			</div>
		{/if}
		<div class="row">
			<label for="mail-subject" class="lbl">{t('mail.compose.subject')}</label>
			<input id="mail-subject" class="field" type="text" bind:value={subject} oninput={touch} />
		</div>
		<label class="sr-only" for="mail-text">{t('mail.compose.text')}</label>
		<textarea
			id="mail-text"
			class="field text"
			bind:value={text}
			oninput={touch}
			rows="14"
			spellcheck="true"></textarea>

		<div class="row">
			<label for="mail-signature" class="lbl">{t('mail.compose.signature')}</label>
			<select
				id="mail-signature"
				class="field sig"
				bind:value={signature}
				onchange={() => {
					signatureChosen = true;
					touch();
				}}
			>
				<option value="">{t('mail.compose.noSignature')}</option>
				{#each signatures as s (s.id)}<option value={s.id}>{s.name}</option>{/each}
			</select>
		</div>
		{#if chosenSignature}
			<p class="sigtext" aria-label={t('mail.compose.signaturePreview')}>
				{`-- \n${chosenSignature.text}`}
			</p>
		{/if}

		<section class="files" aria-labelledby="mail-files-head">
			<h2 id="mail-files-head">{t('mail.compose.attachments')}</h2>
			<div class="file-btns">
				<label class="btn upload"
					><Icon name="upload" size={16} />{t('mail.compose.upload')}<input
						type="file"
						multiple
						class="sr-only"
						onchange={addFiles}
					/></label
				>
				<button type="button" class="btn" onclick={() => (pickingNas = true)}
					><Icon name="folder" size={16} />{t('mail.compose.fromNas')}</button
				>
			</div>
			<ul>
				{#each files as f (f)}
					<li>
						<Icon name="clip" size={16} /><span class="fname">{f.name}</span><span class="muted"
							>{formatBytes(f.size)}</span
						>
						<button
							type="button"
							class="btn x"
							aria-label={t('mail.compose.removeFile', { name: f.name })}
							onclick={() => removeFile(f)}><Icon name="close" size={14} /></button
						>
					</li>
				{/each}
				{#each nasFiles as ref (ref)}
					<li>
						<Icon name="folder" size={16} /><span class="fname">{nasName(ref)}</span><span
							class="muted fref">{ref}</span
						>
						<button
							type="button"
							class="btn x"
							aria-label={t('mail.compose.removeFile', { name: nasName(ref) })}
							onclick={() => removeNas(ref)}><Icon name="close" size={14} /></button
						>
					</li>
				{/each}
				{#each carried?.parts ?? [] as a (a.part)}
					<li>
						<Icon name={carried?.draft ? 'clip' : 'forward'} size={16} /><span class="fname"
							>{a.name || t('mail.attachment.noName')}</span
						><span class="muted"
							>{formatBytes(a.size)} · {carried?.draft
								? t('mail.compose.fromDraft')
								: t('mail.compose.forwarded')}</span
						>
						<button
							type="button"
							class="btn x"
							aria-label={t('mail.compose.removeFile', { name: a.name })}
							onclick={() => removeCarried(a)}><Icon name="close" size={14} /></button
						>
					</li>
				{/each}
			</ul>
			{#if bytes > MAX_BYTES}
				<p class="notice-warn">
					{t('mail.compose.tooLarge', { size: formatBytes(bytes), max: formatBytes(MAX_BYTES) })}
				</p>
			{/if}
		</section>

		{#if sendError}<p class="notice-error send-error" role="alert">{sendError}</p>{/if}
		<p class="muted draft" role="status">
			{#if draftError}<span class="draft-error">{draftError}</span>{:else}{draftNote ||
					t('mail.compose.draftAuto')}{/if}
		</p>
	</form>
{/if}

{#if pickingNas}
	<NasPicker onpick={addNas} onclose={() => (pickingNas = false)} />
{/if}

{#if confirmDiscard}
	<Confirm
		title={t('mail.compose.discardTitle')}
		text={draftUid ? t('mail.compose.discardDraft') : t('mail.compose.discardText')}
		confirm={t('mail.compose.discard')}
		danger
		busy={discarding}
		onconfirm={discard}
		onclose={() => (confirmDiscard = false)}
	/>
{/if}

<style>
	.compose {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		max-width: 60rem;
		min-width: 0;
	}
	.row {
		display: grid;
		grid-template-columns: 4.5rem minmax(0, 1fr);
		align-items: center;
		gap: var(--space-2);
	}
	.lbl {
		color: var(--muted);
		font-size: var(--text-md);
	}
	.row .field {
		width: 100%;
		min-width: 0;
	}
	.sig {
		width: auto !important;
		justify-self: start;
		max-width: 100%;
	}
	.copies {
		justify-self: start;
	}
	.text {
		width: 100%;
		min-height: 16rem;
		resize: vertical;
		font: inherit;
		font-size: var(--text-md);
		line-height: 1.5;
	}
	.sigtext {
		margin: 0;
		padding: var(--space-2) var(--space-3);
		border-left: 3px solid var(--border);
		color: var(--muted);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.files h2 {
		margin: 0 0 var(--space-1-5);
		font-size: var(--text-md);
	}
	.file-btns {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.upload,
	.file-btns .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		cursor: pointer;
	}
	.upload:focus-within {
		outline: 2px solid var(--accent);
	}
	.files ul {
		list-style: none;
		margin: var(--space-2) 0 0;
		padding: 0;
	}
	.files li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
		padding: var(--space-1) 0;
		border-top: 1px solid var(--border);
	}
	.fname {
		flex: 1 1 8rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.fref {
		flex: 1 1 100%;
		order: 3;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.x {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 0;
		width: 32px;
		height: 32px;
	}
	.send-error {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.draft {
		margin: 0;
		font-size: var(--text-sm);
	}
	.draft-error {
		color: var(--error-text);
	}
	@media (max-width: 899px) {
		.row {
			grid-template-columns: minmax(0, 1fr);
		}
		.row > span:empty {
			display: none;
		}
		.x {
			width: 44px;
			height: 44px;
		}
		.btn {
			min-height: 44px;
		}
	}
</style>
