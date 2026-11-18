<script lang="ts">
	// 1.11: Kontakte (docs/KALENDER.md, drafts 10–13, 50–53). Without the
	// data volume titand answers 503 and the page says "nicht verfügbar",
	// never an empty list. Desktop: the column (the address book "Kontakte",
	// apart from it the suggestions of T-Mail – only with T-Mail's release –,
	// import, export, iPhone), the list by letter with the search, and the
	// contact or its form. Phone: list, contact, form, one at a time. What is
	// open lives in the address: ?id= (a contact), &edit=1, ?new=1,
	// ?view=suggestions – so the phone's back arrow and the browser's work.
	import { onDestroy } from 'svelte';
	import { afterNavigate, beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { openCompose } from '#lib/tmail/open.ts';
	import { ApiError } from '#lib/api.ts';
	import { clock, viewZone, wallOf } from '#lib/pim/caldates.ts';
	import { eventHref, loadNasZone, openEventWindow } from '#lib/pim/calendar-api.ts';
	import {
		blankForm,
		fold,
		formOf,
		inputOf,
		namesSomeone,
		type Contact,
		type Form,
		type Item,
		type PhotoChoice,
		type Suggestion,
	} from '#lib/pim/contacts.ts';
	import {
		createContact,
		exportVCF,
		fromSuggestion,
		loadContact,
		loadContacts,
		loadSuggestions,
		putPhotoFile,
		putPhotoNas,
		removeContact,
		removePhoto,
		updateContact,
	} from '#lib/pim/contacts-api.ts';
	import { countText } from '#lib/pim/contacts-text.ts';
	import ContactDetail from '#lib/pim/con/ContactDetail.svelte';
	import ContactEditor from '#lib/pim/con/ContactEditor.svelte';
	import ContactList from '#lib/pim/con/ContactList.svelte';
	import ImportDialog from '#lib/pim/con/ImportDialog.svelte';
	import SuggestionRow from '#lib/pim/con/SuggestionRow.svelte';
	import ProgramShell from '#lib/pim/ProgramShell.svelte';
	import { shellOfError, type ShellState } from '#lib/pim/shell.ts';

	const phone = $derived(phoneView());
	/** T-Mail's release (session.services.mail comes from GET /mail/access). */
	const mail = $derived(session.services.mail === true);
	const bookName = $derived(t('contacts.side.contacts'));

	// ---- What is open (the address) ----
	const params = $derived(page.url.searchParams);
	const view = $derived(params.get('view') === 'suggestions' ? 'suggestions' : 'contacts');
	const selId = $derived(params.get('id') ?? '');
	const mode = $derived.by((): 'none' | 'show' | 'edit' | 'new' => {
		if (view === 'suggestions') return 'none';
		if (params.get('new') === '1') return 'new';
		if (!selId) return 'none';
		return params.get('edit') === '1' ? 'edit' : 'show';
	});
	const editing = $derived(mode === 'edit' || mode === 'new');
	const here = $derived(page.url.pathname + page.url.search);
	function go(q: Record<string, string>, replace = false) {
		const s = new URLSearchParams(q).toString();
		void goto('/contacts' + (s ? '?' + s : ''), { replace });
	}

	// ---- Data ----
	let shown = $state<ShellState>({ kind: 'loading' });
	let all = $state<Item[]>([]);
	let zone = $state(viewZone());
	let clockNow = $state(new Date());
	const timer = setInterval(() => (clockNow = new Date()), 60_000);
	onDestroy(() => clearInterval(timer));
	const today = $derived(wallOf(clockNow, zone).date);
	let notice = $state('');

	function failed(err: unknown): string {
		const s = shellOfError(err);
		if (s.kind === 'unavailable') shown = s;
		return apiErrorText(err);
	}

	async function start() {
		void loadNasZone().then((z) => (zone = z));
		try {
			all = await loadContacts();
			shown = { kind: 'ready' };
		} catch (err) {
			shown = shellOfError(err);
		}
	}
	$effect(() => {
		void start();
	});

	async function refreshList() {
		try {
			all = await loadContacts();
		} catch (err) {
			notice = failed(err);
		}
		if (answered) void search(answered);
		if (sugAll !== null) void loadAllSuggestions();
	}

	// ---- Search (contacts and, with T-Mail, its suggestions) ----
	let query = $state('');
	let answered = $state('');
	let hits = $state<Item[]>([]);
	let sugHits = $state<Suggestion[]>([]);
	let asked = 0;
	let wait: ReturnType<typeof setTimeout> | undefined;
	function onquery(q: string) {
		query = q;
		clearTimeout(wait);
		wait = setTimeout(() => void search(q), q.trim() ? 200 : 0);
	}
	onDestroy(() => clearTimeout(wait));
	async function search(q: string) {
		const mine = ++asked;
		const want = q.trim();
		if (!want) {
			answered = '';
			hits = [];
			sugHits = [];
			return;
		}
		try {
			const [found, sug] = await Promise.all([
				loadContacts(want),
				mail ? loadSuggestions(want).catch(() => [] as Suggestion[]) : ([] as Suggestion[]),
			]);
			if (mine !== asked) return;
			hits = found;
			sugHits = sug;
			answered = want;
			notice = '';
		} catch (err) {
			if (mine !== asked) return;
			notice = failed(err);
		}
	}
	const items = $derived(answered ? hits : all);
	const soonOf = (id: string) =>
		(all.find((i) => i.id === id) ?? hits.find((i) => i.id === id))?.birthdaySoon ?? null;

	// ---- Suggestions of T-Mail (the column's entry, ?view=suggestions) ----
	let sugAll = $state<Suggestion[] | null>(null);
	let sugFilter = $state('');
	let sugError = $state('');
	let taking = $state('');
	async function loadAllSuggestions() {
		try {
			sugAll = await loadSuggestions();
			sugError = '';
		} catch (err) {
			sugError = failed(err);
		}
	}
	$effect(() => {
		if (shown.kind === 'ready' && mail && sugAll === null) void loadAllSuggestions();
	});
	const sugShown = $derived.by(() => {
		const f = fold(sugFilter.trim());
		const list = sugAll ?? [];
		return f ? list.filter((s) => fold(`${s.name} ${s.addr}`).includes(f)) : list;
	});
	async function take(s: Suggestion) {
		taking = s.addr;
		notice = '';
		try {
			const made = await fromSuggestion(s.addr);
			await refreshList();
			go({ id: made.id });
		} catch (err) {
			notice = failed(err);
		} finally {
			taking = '';
		}
	}

	// ---- The open contact ----
	let contact = $state<Contact | null>(null);
	let contactError = $state('');
	let loadingContact = 0;
	async function openContact(id: string) {
		const mine = ++loadingContact;
		contactError = '';
		try {
			const got = await loadContact(id);
			if (mine === loadingContact) contact = got;
		} catch (err) {
			if (mine !== loadingContact) return;
			contact = null;
			contactError = failed(err);
		}
	}
	$effect(() => {
		if (shown.kind !== 'ready') return;
		if (selId && mode !== 'new') {
			if (contact?.id !== selId) void openContact(selId);
		} else {
			contact = null;
			contactError = '';
		}
	});
	// The chosen row in sight (once per contact).
	let scrolledTo = '';
	$effect(() => {
		if (phone || !selId || selId === scrolledTo || shown.kind !== 'ready' || !all.length) return;
		scrolledTo = selId;
		requestAnimationFrame(() =>
			document.querySelector('.crow.sel')?.scrollIntoView({ block: 'center' }),
		);
	});

	// ---- The form ----
	let form = $state<Form>(blankForm());
	let before = $state<Form | null>(null);
	let photo = $state<PhotoChoice>({ kind: 'keep' });
	let busy = $state(false);
	let formError = $state('');
	let conflict = $state(false);
	let formKey = '';
	const editKey = $derived(
		mode === 'new'
			? 'new'
			: mode === 'edit' && contact?.id === selId
				? `edit:${selId}:${contact.etag}`
				: '',
	);
	$effect(() => {
		const key = editKey;
		if (key === formKey) return;
		formKey = key;
		if (!key) return;
		if (photo.kind === 'file') URL.revokeObjectURL(photo.preview);
		photo = { kind: 'keep' };
		formError = '';
		conflict = false;
		if (key === 'new') {
			form = blankForm();
			before = null;
		} else if (contact) {
			form = formOf(contact);
			before = formOf(contact);
		}
	});
	// "Bitte einen Namen …" goes once the form names someone.
	$effect(() => {
		if (formError && formError === t('contacts.form.nobody') && namesSomeone(form)) formError = '';
	});
	const dirty = $derived(
		editing &&
			editKey !== '' &&
			(photo.kind !== 'keep' || JSON.stringify(form) !== JSON.stringify(before ?? blankForm())),
	);

	// Leaving the form with changes asks first (another contact, the back arrow …).
	let leaving = false;
	let pendingHref = $state('');
	guardWindowClose(() => (busy ? 'busy' : dirty ? 'edits' : ''));
	beforeNavigate((nav) => {
		if (leaving || !dirty || nav.willUnload || !nav.to) return;
		nav.cancel();
		pendingHref = nav.to.url.pathname + nav.to.url.search;
	});
	afterNavigate(() => {
		leaving = false;
	});
	function leave(href: string) {
		leaving = true;
		void goto(href, { replace: true });
	}
	function cancel() {
		leave(mode === 'edit' || selId ? `/contacts?id=${encodeURIComponent(selId)}` : '/contacts');
	}

	async function save() {
		if (!namesSomeone(form)) {
			formError = t('contacts.form.nobody');
			return;
		}
		busy = true;
		formError = '';
		conflict = false;
		try {
			let id: string;
			let etag: string;
			if (mode === 'new') {
				const made = await createContact(inputOf(form, null));
				id = made.id;
				etag = made.etag;
			} else {
				if (!contact) return;
				id = contact.id;
				etag = contact.etag;
				const input = inputOf(form, before);
				if (Object.keys(input).length) etag = (await updateContact(id, etag, input)).etag;
			}
			// The picture after the card, with the card's new etag.
			let photoNote = '';
			try {
				if (photo.kind === 'file') await putPhotoFile(id, etag, photo.file);
				else if (photo.kind === 'nas') await putPhotoNas(id, etag, photo.root, photo.path);
				else if (photo.kind === 'remove') await removePhoto(id, etag);
			} catch (err) {
				photoNote = t('contacts.form.photoFailed', { error: apiErrorText(err) });
			}
			if (photo.kind === 'file') URL.revokeObjectURL(photo.preview);
			photo = { kind: 'keep' };
			contact = null;
			await refreshList();
			leave(`/contacts?id=${encodeURIComponent(id)}`);
			notice = photoNote;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'contacts_conflict') {
				conflict = true;
				formError = t('contacts.conflict');
			} else formError = failed(err);
		} finally {
			busy = false;
		}
	}
	async function reloadContact() {
		if (!selId) return;
		conflict = false;
		formError = '';
		await openContact(selId);
	}

	// ---- Deleting ----
	let askDelete = $state(false);
	let delError = $state('');
	let delConflict = $state(false);
	async function remove() {
		if (!contact) return;
		busy = true;
		delError = '';
		try {
			await removeContact(contact.id, contact.etag);
			askDelete = false;
			contact = null;
			await refreshList();
			leave('/contacts');
		} catch (err) {
			delConflict = err instanceof ApiError && err.code === 'contacts_conflict';
			delError = delConflict ? t('contacts.conflict') : failed(err);
		} finally {
			busy = false;
		}
	}

	// ---- Actions of a contact ----
	function writeMail(addr: string) {
		openCompose({ a: '', to: addr, back: '/mail' });
	}
	function newEvent() {
		if (!contact) return;
		const now = wallOf(new Date(), zone).minutes;
		const time = clock(Math.min(23 * 60, (Math.floor(now / 60) + 1) * 60));
		openEventWindow(
			eventHref({ date: today, time, title: contact.name || contact.org, back: here }),
		);
	}

	// ---- Import, export ----
	let importing = $state(false);
	async function runExport() {
		notice = '';
		try {
			await exportVCF();
		} catch (err) {
			notice = failed(err);
		}
	}

	registerMenuCommands({
		'contacts.new': {
			run: () => go(selId ? { new: '1', id: selId } : { new: '1' }),
			enabled: () => shown.kind === 'ready',
		},
	});

	const title = $derived(
		mode === 'new'
			? t('contacts.newTitle')
			: mode === 'edit'
				? t('contacts.editTitle')
				: view === 'suggestions'
					? t('contacts.suggestions')
					: t('desktop.app.contacts'),
	);
</script>

{#snippet bookActions()}
	<button type="button" class="btn side" onclick={() => (importing = true)}
		><Icon name="upload" size={15} />{t('contacts.import')}</button
	>
	<button type="button" class="btn side" onclick={runExport}
		><Icon name="download" size={15} />{t('contacts.export')}</button
	>
	<a class="btn side" href="/contacts/iphone"
		><Icon name="device" size={15} />{t('calendar.side.iphone')}</a
	>
{/snippet}

{#snippet list()}
	<ContactList
		{items}
		total={all.length}
		{query}
		{answered}
		suggestions={sugHits}
		selected={phone ? '' : selId}
		{today}
		{phone}
		{taking}
		{onquery}
		onopen={(item) => go({ id: item.id })}
		ontake={take}
	/>
{/snippet}

{#snippet suggestions()}
	<div class="sugs" data-suggestions>
		<h2 class="sh">{t('contacts.sug.title')}</h2>
		{#if !mail}
			<p class="explain" data-suggestions-off>{t('contacts.sug.noMail')}</p>
		{:else}
			<label class="search">
				<Icon name="search" size={16} />
				<input
					class="field"
					type="search"
					placeholder={t('contacts.sug.filter')}
					aria-label={t('contacts.sug.filter')}
					bind:value={sugFilter}
				/>
			</label>
			<p class="muted small">{t('contacts.sug.text')}</p>
			{#if sugError}<p class="notice-error" role="alert">{sugError}</p>{/if}
			{#if sugAll === null && !sugError}
				<p class="muted" role="status">{t('common.loading')}</p>
			{:else if sugAll && !sugAll.length}
				<p class="muted">{t('contacts.sug.empty')}</p>
			{:else if !sugShown.length}
				<p class="muted">{t('contacts.sug.noHits', { q: sugFilter.trim() })}</p>
			{/if}
			{#each sugShown as s (s.addr)}
				<SuggestionRow
					suggestion={s}
					query={sugFilter}
					{phone}
					busy={taking === s.addr}
					ontake={take}
				/>
			{/each}
		{/if}
	</div>
{/snippet}

{#snippet right()}
	{#if editing}
		{#if formError}
			<p class="notice-error msg" role="alert">
				{formError}
				{#if conflict}<button type="button" class="btn" onclick={reloadContact}
						>{t('calendar.reload')}</button
					>{/if}
			</p>
		{/if}
		{#if mode === 'new' || (contact && contact.id === selId)}
			<ContactEditor
				bind:form
				bind:photo
				contact={mode === 'new' ? null : contact}
				{phone}
				{busy}
				onsubmit={save}
				ondelete={() => {
					delError = '';
					delConflict = false;
					askDelete = true;
				}}
			/>
		{:else if contactError}
			<p class="notice-error" role="alert">{contactError}</p>
		{:else}
			<p class="muted" role="status">{t('common.loading')}</p>
		{/if}
	{:else if mode === 'show'}
		{#if contactError}
			<p class="notice-error" role="alert">{contactError}</p>
		{:else if contact && contact.id === selId}
			<ContactDetail
				{contact}
				soon={soonOf(contact.id)}
				{today}
				{phone}
				{mail}
				book={bookName}
				onedit={() => go({ id: selId, edit: '1' })}
				onmail={writeMail}
				onevent={newEvent}
			/>
		{:else}
			<p class="muted" role="status">{t('common.loading')}</p>
		{/if}
	{:else if view === 'suggestions'}
		{#if mail}<p class="muted none">{t('contacts.sug.about')}</p>{/if}
	{:else}
		<p class="muted none">{t('contacts.pick')}</p>
	{/if}
{/snippet}

<PageHead {title}>
	{#snippet actions()}
		{#if editing}
			<button type="button" class="btn" disabled={busy} onclick={cancel}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || shown.kind !== 'ready' || editKey === ''}
				onclick={save}><Icon name="check" size={16} />{t('common.save')}</button
			>
		{:else if phone && mode === 'show'}
			<button
				type="button"
				class="btn"
				disabled={!contact}
				onclick={() => go({ id: selId, edit: '1' })}
				><Icon name="edit" size={16} />{t('contacts.edit')}</button
			>
		{:else}
			<button
				type="button"
				class="btn btn-primary"
				disabled={shown.kind !== 'ready'}
				onclick={() => go(selId ? { new: '1', id: selId } : { new: '1' })}
				><Icon name="plus" size={16} />{t('contacts.new')}</button
			>
		{/if}
	{/snippet}
</PageHead>

<ProgramShell program="contacts" view={shown} own>
	{#if notice}<p class="notice-error msg top" role="alert">{notice}</p>{/if}
	{#if phone}
		<div class="ph" data-pim-ready data-mode={mode} data-view={view}>
			{#if editing || mode === 'show'}
				{@render right()}
			{:else if view === 'suggestions'}
				{@render suggestions()}
			{:else}
				{@render list()}
				<div class="ph-book">
					{@render bookActions()}
				</div>
			{/if}
		</div>
	{:else}
		<div class="con" data-pim-ready data-mode={mode} data-view={view}>
			<nav class="books" aria-label={t('contacts.books')}>
				<h3>{t('contacts.books')}</h3>
				<a
					class="book"
					class:on={view === 'contacts'}
					href="/contacts"
					aria-current={view === 'contacts' ? 'page' : undefined}
					><Icon name="book" size={16} /><span>{bookName}</span><small
						><span class="sr-only">{countText(all.length)}</span><span aria-hidden="true"
							>{all.length}</span
						></small
					></a
				>
				{#if mail}
					<h3>{t('contacts.fromMail')}</h3>
					<a
						class="book"
						class:on={view === 'suggestions'}
						href="/contacts?view=suggestions"
						aria-current={view === 'suggestions' ? 'page' : undefined}
						><Icon name="mail" size={16} /><span>{t('contacts.suggestions')}</span
						>{#if sugAll}<small>{sugAll.length}</small>{/if}</a
					>
				{/if}
				<span class="sp"></span>
				{@render bookActions()}
			</nav>
			<div class="mid">
				{#if view === 'suggestions'}{@render suggestions()}{:else}{@render list()}{/if}
			</div>
			<div class="right">{@render right()}</div>
		</div>
	{/if}
</ProgramShell>

{#if askDelete && contact}
	<Dialog
		title={t('contacts.del.title', { name: contact.name || t('contacts.unnamed') })}
		locked={busy}
		onclose={() => (askDelete = false)}
	>
		<p class="lead">{t('contacts.del.text', { name: contact.name || t('contacts.unnamed') })}</p>
		{#if delError}
			<p class="notice-error" role="alert">
				{delError}
				{#if delConflict}<button
						type="button"
						class="btn"
						onclick={() => {
							askDelete = false;
							void reloadContact();
						}}>{t('calendar.reload')}</button
					>{/if}
			</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (askDelete = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy || delConflict} onclick={remove}
				><Icon name="trash" size={15} />{t('contacts.del.confirm')}</button
			>
		</div>
	</Dialog>
{/if}

{#if pendingHref}
	<Dialog title={t('calendar.form.discardTitle')} onclose={() => (pendingHref = '')}>
		<p class="lead">{t('contacts.discardText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (pendingHref = '')}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					const href = pendingHref;
					pendingHref = '';
					leaving = true;
					void goto(href);
				}}>{t('calendar.form.discard')}</button
			>
		</div>
	</Dialog>
{/if}

{#if importing}
	<ImportDialog onclose={() => (importing = false)} onchanged={() => void refreshList()} />
{/if}

<style>
	.con {
		display: grid;
		grid-template-columns: minmax(190px, 230px) minmax(260px, 340px) minmax(0, 1fr);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		/* Like T-Mail: the columns scroll on their own. */
		height: calc(100dvh - 8.5rem);
		min-height: 420px;
	}
	.con > * {
		min-width: 0;
		min-height: 0;
		overflow: auto;
	}
	.con > * + * {
		border-left: 1px solid var(--border);
	}
	.books {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-3) var(--space-2);
	}
	.books h3 {
		margin: var(--space-3) var(--space-2) var(--space-1);
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.books h3:first-child {
		margin-top: 0;
	}
	.book {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 40px;
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.book:hover {
		background: var(--srow-hover);
	}
	.book span {
		flex: 1;
		min-width: 0;
		overflow-wrap: break-word;
	}
	.book small {
		color: var(--muted);
	}
	.book.on {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.sp {
		flex: 1;
		min-height: var(--space-4);
	}
	.side {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		align-self: flex-start;
		margin: 2px var(--space-1);
		text-decoration: none;
	}
	.right {
		padding: var(--space-4);
	}
	.none {
		margin: var(--space-4);
		overflow-wrap: break-word;
	}
	.msg {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.msg.top {
		margin: 0 0 var(--space-3);
	}
	.right .msg {
		margin: 0 0 var(--space-3);
	}
	.lead {
		margin: 0;
		overflow-wrap: break-word;
	}
	.sugs {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
	}
	.ph .sugs {
		padding: 0;
	}
	.sugs p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.sh {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.explain {
		line-height: 1.5;
	}
	.search {
		position: relative;
		display: flex;
		align-items: center;
		color: var(--muted);
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
	}
	.search .field {
		width: 100%;
		padding-left: 34px;
		min-height: 40px;
	}
	.ph .search .field {
		min-height: 44px;
	}
	.small {
		font-size: var(--text-sm);
	}
	.ph-book {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-5);
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}
	.ph-book .side {
		margin: 0;
		min-height: 44px;
	}
	.ph :global(.btn) {
		min-height: 44px;
	}
	/* A smaller window: the address book as a row on top, so the contact keeps room. */
	@media (max-width: 1100px) {
		.con {
			grid-template-columns: minmax(240px, 320px) minmax(0, 1fr);
			grid-template-rows: auto minmax(0, 1fr);
			grid-template-areas: 'books books' 'mid right';
		}
		.books {
			grid-area: books;
			flex-direction: row;
			flex-wrap: wrap;
			align-items: center;
			gap: var(--space-1) var(--space-2);
			border-bottom: 1px solid var(--border);
		}
		.books h3 {
			display: none;
		}
		.mid {
			grid-area: mid;
			border-left: 0 !important;
		}
		.right {
			grid-area: right;
		}
		.sp {
			display: none;
		}
		.side {
			align-self: center;
		}
	}
	/* A narrow window: list and contact below each other. */
	@media (max-width: 640px) {
		.con {
			grid-template-columns: minmax(0, 1fr);
			grid-template-rows: none;
			grid-template-areas: 'books' 'mid' 'right';
			height: auto;
		}
		.right {
			border-left: 0 !important;
			border-top: 1px solid var(--border);
		}
	}
</style>
