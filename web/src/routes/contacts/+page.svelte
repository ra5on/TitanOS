<script lang="ts">
	// 1.11: Kontakte (docs/KALENDER.md, drafts 10–13, 50–53). Without the
	// data volume titand answers 503 and the page says "nicht verfügbar",
	// never an empty list.
	// 1.13 (owner 18 Nov 2026, docs/KALENDER.md "Kontakte 1.13"): on the
	// shared ColumnFrame like Mail – edge to edge, the window's height, a
	// side bar with "Neuer Kontakt" on top, the address books (sorted per
	// account), the groups (iOS's group vCards), Mail's suggestions and the
	// possible duplicates; the list with its search on top of its column;
	// the contact, its form or the merge of two. No strip above, the title
	// bar names the place. On the phone and under 620 px one level at a time
	// (contact-levels.ts) without cards, the actions in a bar at the bottom,
	// "Neuer Kontakt" at its right, the form a sheet over the whole screen.
	// What is open lives in the address, so the back arrows work. Requests:
	// the list (with its ETag: unchanged is a 304) and the side bar's
	// overview on opening; Mail's suggestions only when shown; a search is
	// one request; a write puts its row in place (no list again); the live
	// stream (lib/pim/live.ts) reads the list when a device changed it.
	// The KI-Agent (1.13, with session.services.agent): its panel right of
	// the contact (a column of the frame) or a sheet from below, open or not
	// kept per device; the open contact goes with each message as a
	// reference (book and id), never its content. A draft (?agentDraft=,
	// also while the page is open) fills the form; saved only by the person.
	// 1.15 (PLAN 115-6, drafts 40–44 in Entwuerfe/115-mail-handy, owner 19
	// Nov 2026 "desktop like mobile", "windows resizable"): Apple's Kontakte
	// – "Listen" (all contacts, address books, groups, Mehr), the list with
	// the big title, the search, "Meine Karte", sections by the family name
	// and the register A–Z, the contact with its quick buttons, the form
	// as Apple's sheet. The layout follows the window (each desktop window
	// is a page of its own, resized freely): from 620 px columns side by
	// side (Listen | Kontakte | Karte), under 900 px the side bar folds to
	// its rail (ColumnFrame foldBelow); under 620 px and on the phone one
	// level at a time with "‹ Listen" / "‹ Kontakte" on top. No bar at the
	// bottom any more: "+" stands on top of the list, "Bearbeiten" on top
	// of the contact, the KI-Agent in the program's ⋯ menu (the phone's bar
	// and the window's menu bar, command "contacts.agent") – and, where
	// neither shows it, in a ⋯ of the page's own; at the side bar's foot
	// in the columns as before.
	import { onDestroy, onMount, untrack } from 'svelte';
	import { afterNavigate, beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import ColumnFrame, { type FrameLevel } from '#lib/components/ColumnFrame.svelte';
	import { compactView } from '#lib/components/compact.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { entries, menusFor } from '#lib/desktop/menubar/menus.ts';
	import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { openCompose } from '#lib/tmail/open.ts';
	import { api, ApiError } from '#lib/api.ts';
	import { clock, viewZone, wallOf } from '#lib/pim/caldates.ts';
	import { eventHref, loadNasZone, openEventWindow } from '#lib/pim/calendar-api.ts';
	import {
		contactLevelOf,
		contactPlaceOf,
		contactsHref,
		contactsUp,
		type ContactPlace,
	} from '#lib/pim/contact-levels.ts';
	import {
		blankForm,
		fold,
		formOf,
		groupsOf,
		inputOf,
		namesSomeone,
		upsertItem,
		type Contact,
		type DuplicateSet,
		type Form,
		type Group,
		type Item,
		type Overview,
		type PhotoChoice,
		type Suggestion,
	} from '#lib/pim/contacts.ts';
	import {
		createContact,
		createGroup,
		exportVCF,
		fromSuggestion,
		keepListTag,
		knowGeneration,
		knownGeneration,
		loadContact,
		loadContacts,
		loadDuplicates,
		loadOverview,
		loadSuggestions,
		putPhotoFile,
		putPhotoNas,
		removeContact,
		removeGroup,
		removePhoto,
		rowOf,
		searchContacts,
		startContactList,
		updateContact,
		updateGroup,
		type Saved,
	} from '#lib/pim/contacts-api.ts';
	import { dupText } from '#lib/pim/contacts-text.ts';
	import { watchPim } from '#lib/pim/live.ts';
	import ContactDetail from '#lib/pim/con/ContactDetail.svelte';
	import ContactEditor from '#lib/pim/con/ContactEditor.svelte';
	import ContactList from '#lib/pim/con/ContactList.svelte';
	import ContactRail from '#lib/pim/con/ContactRail.svelte';
	import ContactSide, { type SidePlace } from '#lib/pim/con/ContactSide.svelte';
	import ImportDialog from '#lib/pim/con/ImportDialog.svelte';
	import MergeView from '#lib/pim/con/MergeView.svelte';
	import SuggestionRow from '#lib/pim/con/SuggestionRow.svelte';
	import { contactDraftForm, loadAgentDraft, type AgentDraft } from '#lib/agent/drafts.ts';
	import AgentPanel from '#lib/components/agent/AgentPanel.svelte';
	import AgentSheet from '#lib/components/agent/BottomSheet.svelte';
	import type { AgentContext } from '#lib/agent.ts';
	import { contactsPanelActions } from '#lib/agent/panel-actions.ts';
	import { shellOfError, type ShellState } from '#lib/pim/shell.ts';

	// ---- Columns or levels: the window's width decides (1.15) ----
	const phone = $derived(phoneView());
	/** The phone or under 620 px: one level at a time. */
	const compact = $derived(compactView());
	/** Columns side by side (620 px and more); `wide` keeps its old name for them. */
	const wide = $derived(!compact);
	/** Mail's release (session.services.mail): "E-Mail schreiben" opens its compose. */
	const mail = $derived(session.services.mail === true);
	const bookName = $derived(t('contacts.side.contacts'));

	// ---- What is open (the address) ----
	const place = $derived(contactPlaceOf(page.url));
	const level = $derived(contactLevelOf(place));
	const view = $derived(place.view);
	const selId = $derived(place.id);
	const mode = $derived.by((): 'none' | 'show' | 'edit' | 'new' => {
		if (place.isNew) return 'new';
		if (!selId) return 'none';
		return place.edit ? 'edit' : 'show';
	});
	const editing = $derived(mode === 'edit' || mode === 'new');
	const here = $derived(page.url.pathname + page.url.search);
	/** Goes to a place; levels are steps in the history, columns replace it. */
	function go(p: Partial<ContactPlace>, push = !wide) {
		void goto(contactsHref(p), { replace: !push });
	}
	/** The list's place now (the book, a group, suggestions or duplicates). */
	const listPlace = $derived<Partial<ContactPlace>>({ view: place.view, g: place.g });

	// ---- Data ----
	let shown = $state<ShellState>({ kind: 'loading' });
	let all = $state<Item[]>([]);
	let overview = $state<Overview | null>(null);
	let zone = $state(viewZone());
	let clockNow = $state(new Date());
	const timer = setInterval(() => (clockNow = new Date()), 60_000);
	onDestroy(() => clearInterval(timer));
	const today = $derived(wallOf(clockNow, zone).date);
	let notice = $state('');
	let addedTo = $state('');
	/** Pictures changed here (id → etag): the list loads them anew. */
	let pictures = $state<Record<string, string>>({});
	const groups = $derived(overview?.groups ?? []);
	const group = $derived(place.g ? groups.find((g) => g.id === place.g) : undefined);

	function failed(err: unknown): string {
		const s = shellOfError(err);
		if (s.kind === 'unavailable') shown = s;
		return apiErrorText(err);
	}

	async function start() {
		// 115-7: the list's ETag of an earlier visit (the module stays) would make this a 304.
		startContactList();
		try {
			const [list, over] = await Promise.all([loadContacts(), loadOverview()]);
			all = list ?? all;
			overview = over;
			shown = { kind: 'ready' };
		} catch (err) {
			shown = shellOfError(err);
		}
	}
	onMount(() => void start());
	// 1.13: a draft of the KI-Agent (?agentDraft=, "Öffnen und bearbeiten"):
	// the form opens with it filled in, saved only by the person. Read from
	// the address whenever it names one – the panel of this page opens it
	// here too, without loading the page anew.
	let agentDraft: AgentDraft | null = null;
	let draftSeen = '';
	/** The form shows a draft of the agent. */
	let fromAgent = $state(false);
	let draftNote = $state('');
	$effect(() => {
		const param = page.url.searchParams.get('agentDraft') ?? '';
		if (!param || param === draftSeen) return;
		draftSeen = param;
		untrack(() => void takeDraft(param));
	});
	async function takeDraft(param: string) {
		draftNote = '';
		const d = await loadAgentDraft(param);
		draftSeen = '';
		if (d?.app !== 'contacts' || d.kind !== 'contact') {
			draftNote = d ? t('agentpim.draft.foreign') : t('agentpim.draft.gone');
			go(listPlace, false);
			return;
		}
		agentDraft = d;
		const id = typeof d.fields.id === 'string' ? d.fields.id : '';
		// The form already open for it (and unchanged): the draft goes in here.
		if (!dirty && editKey !== '' && (id ? mode === 'edit' && selId === id : mode === 'new')) {
			form = contactDraftForm(form, d.fields);
			agentDraft = null;
			fromAgent = true;
		}
		go(id ? { ...listPlace, id, edit: true } : { ...listPlace, isNew: true }, false);
	}
	// The NAS zone (GET /calendar/access) only when a birthday of this week is worded.
	let zoneAsked = false;
	$effect(() => {
		if (zoneAsked || !all.some((i) => i.birthdaySoon)) return;
		zoneAsked = true;
		void loadNasZone().then((z) => (zone = z));
	});

	/** The list again (a 304 when nothing changed) and the side bar; false when it failed. */
	async function refresh(): Promise<boolean> {
		try {
			const [list, over] = await Promise.all([loadContacts(), loadOverview()]);
			if (list) all = list;
			overview = over;
			if (shown.kind !== 'ready') shown = { kind: 'ready' };
		} catch (err) {
			notice = failed(err);
			return false;
		}
		if (answered) void search(answered);
		if (sugAll !== null) void loadAllSuggestions();
		if (dups !== null) void loadDups();
		// The open contact changed elsewhere (an iPhone): read again unless it is being edited.
		const row = all.find((i) => i.id === contact?.id);
		if (contact && !editing && row?.etag && row.etag !== contact.etag) void openContact(contact.id);
		return true;
	}
	/** The side bar's counts after a change of this page. */
	async function refreshOverview() {
		try {
			overview = await loadOverview();
		} catch (err) {
			notice = failed(err);
		}
	}

	// Live: a change of the address book on a device (or another window)
	// reads the list again. An event naming the generation the list has
	// seen (this page's own write) is skipped; "ready" names the
	// generations when the stream opens (the list read once more with its
	// ETag, a 304 when nothing changed meanwhile).
	let liveTimer: ReturnType<typeof setTimeout> | undefined;
	let liveGen = '';
	onMount(() => {
		const stop = watchPim((e) => {
			if (e.type === 'ready') liveGen = e.generations['contacts/kontakte'] ?? '';
			else if (e.type === 'pim') {
				if (e.kind !== 'contacts') return;
				if (e.collection === 'kontakte' && e.generation && e.generation === knownGeneration())
					return;
				liveGen = e.collection === 'kontakte' ? e.generation : '';
			} else liveGen = '';
			clearTimeout(liveTimer);
			liveTimer = setTimeout(() => {
				const gen = liveGen;
				knowGeneration('');
				void refresh().then((ok) => ok && knowGeneration(gen));
			}, 300);
		});
		return () => {
			clearTimeout(liveTimer);
			stop();
		};
	});

	/** A row written by this page: put in place, the list's ETag kept. */
	function written(saved: Saved) {
		const row = rowOf(saved);
		if (row) all = upsertItem(all, row);
		if (row && hits.length) hits = hits.map((h) => (h.id === row.id ? row : h));
		keepListTag(saved.listETag, saved.ctag);
	}
	function dropRow(id: string) {
		all = all.filter((i) => i.id !== id);
		hits = hits.filter((i) => i.id !== id);
	}

	// ---- Search: one request with Mail's suggestions ----
	let query = $state('');
	let answered = $state('');
	let hits = $state<Item[]>([]);
	let sugHits = $state<Suggestion[]>([]);
	let searching: AbortController | null = null;
	let wait: ReturnType<typeof setTimeout> | undefined;
	function onquery(q: string) {
		query = q;
		clearTimeout(wait);
		wait = setTimeout(() => void search(q), q.trim() ? 200 : 0);
	}
	onDestroy(() => {
		clearTimeout(wait);
		searching?.abort();
	});
	async function search(q: string) {
		searching?.abort();
		const want = q.trim();
		if (!want) {
			answered = '';
			hits = [];
			sugHits = [];
			return;
		}
		const own = (searching = new AbortController());
		try {
			const found = await searchContacts(want, own.signal);
			if (own.signal.aborted) return;
			hits = found.contacts;
			sugHits = mail ? found.suggestions : [];
			answered = want;
			notice = '';
		} catch (err) {
			if (own.signal.aborted) return;
			notice = failed(err);
		}
	}
	const inGroup = $derived(group ? new Set(group.members) : null);
	const items = $derived.by(() => {
		const list = answered ? hits : all;
		return inGroup ? list.filter((i) => inGroup.has(i.id)) : list;
	});
	const total = $derived(inGroup ? all.filter((i) => inGroup.has(i.id)).length : all.length);
	const soonOf = (id: string) =>
		(all.find((i) => i.id === id) ?? hits.find((i) => i.id === id))?.birthdaySoon ?? null;
	const nameOf = (id: string) => all.find((i) => i.id === id)?.name || t('contacts.unnamed');

	// ---- Suggestions of Mail (only when shown) ----
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
		if (shown.kind === 'ready' && mail && view === 'suggestions' && sugAll === null)
			untrack(() => void loadAllSuggestions());
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
			written(made);
			if (sugAll) sugAll = sugAll.filter((x) => x.addr !== s.addr);
			sugHits = sugHits.filter((x) => x.addr !== s.addr);
			void refreshOverview();
			go({ id: made.id }, !wide);
		} catch (err) {
			notice = failed(err);
		} finally {
			taking = '';
		}
	}

	// ---- Possible duplicates (only when shown) ----
	let dups = $state<DuplicateSet[] | null>(null);
	let dupError = $state('');
	const HIDDEN_KEY = 'titanos.contacts.dupHidden';
	let hidden = $state<string[]>(readHidden());
	function readHidden(): string[] {
		try {
			const v = JSON.parse(localStorage.getItem(HIDDEN_KEY) ?? '[]');
			return Array.isArray(v) ? v.filter((x) => typeof x === 'string').slice(0, 500) : [];
		} catch {
			return [];
		}
	}
	const setKey = (s: DuplicateSet) => [...s.ids].sort().join('\u0000');
	function hideSet(s: DuplicateSet) {
		hidden = [...hidden.filter((k) => k !== setKey(s)), setKey(s)].slice(-500);
		try {
			localStorage.setItem(HIDDEN_KEY, JSON.stringify(hidden));
		} catch {
			// Kept for this page only.
		}
		go({ view: 'duplicates' }, false);
	}
	const dupShown = $derived((dups ?? []).filter((s) => !hidden.includes(setKey(s))));
	const openSet = $derived(place.set ? dupShown.find((s) => s.ids.includes(place.set)) : undefined);
	async function loadDups() {
		try {
			dups = await loadDuplicates();
			dupError = '';
		} catch (err) {
			dupError = failed(err);
		}
	}
	// Read when shown – or at once when sets are hidden on this device, so
	// the side bar's count leaves them out (and the entry goes with the last).
	$effect(() => {
		if (shown.kind !== 'ready' || dups !== null) return;
		if (view === 'duplicates' || (hidden.length && (overview?.duplicates ?? 0) > 0))
			untrack(() => void loadDups());
	});
	// Wide: the first set opens by itself.
	$effect(() => {
		if (!wide || view !== 'duplicates' || place.set || !dupShown.length) return;
		const first = dupShown[0].ids[0];
		untrack(() => go({ view: 'duplicates', set: first }, false));
	});
	async function merged(keep: string, drop: string, saved: Saved) {
		written(saved);
		dropRow(drop);
		if (selId === drop) contact = null;
		await Promise.all([loadDups(), refreshOverview()]);
		const next = dupShown[0]?.ids[0];
		if (next) go({ view: 'duplicates', set: next }, false);
		else go({ id: keep }, false);
	}

	// ---- The open contact ----
	let contact = $state<Contact | null>(null);
	let contactError = $state('');
	let loadingContact = 0;
	/** The contact being read now: an effect running again asks once. */
	let reading = '';
	async function openContact(id: string) {
		const mine = ++loadingContact;
		reading = id;
		contactError = '';
		try {
			const got = await loadContact(id);
			if (mine === loadingContact) contact = got;
		} catch (err) {
			if (mine !== loadingContact) return;
			contact = null;
			contactError = failed(err);
		} finally {
			if (mine === loadingContact) reading = '';
		}
	}
	$effect(() => {
		if (shown.kind !== 'ready') return;
		if (selId && mode !== 'new') {
			if (contact?.id !== selId && reading !== selId) untrack(() => void openContact(selId));
		} else {
			contact = null;
			contactError = '';
		}
	});
	// The chosen row in sight (once per contact).
	let scrolledTo = '';
	$effect(() => {
		if (!wide || !selId || selId === scrolledTo || shown.kind !== 'ready' || !all.length) return;
		scrolledTo = selId;
		requestAnimationFrame(() =>
			document.querySelector('.crow.sel')?.scrollIntoView({ block: 'center' }),
		);
	});
	const contactGroups = $derived(contact ? groupsOf(groups, contact.id) : []);

	// ---- The form ----
	let form = $state<Form>(blankForm());
	let before = $state<Form | null>(null);
	let photo = $state<PhotoChoice>({ kind: 'keep' });
	let member = $state<string[]>([]);
	let memberBefore = $state<string[]>([]);
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
		untrack(() => {
			if (photo.kind === 'file') URL.revokeObjectURL(photo.preview);
			photo = { kind: 'keep' };
			formError = '';
			conflict = false;
			fromAgent = false;
			if (key === 'new') {
				form = blankForm();
				if (agentDraft && !agentDraft.fields.id) {
					form = contactDraftForm(form, agentDraft.fields);
					agentDraft = null;
					fromAgent = true;
				}
				before = null;
				// A new contact in an open group joins it.
				member = place.g ? [place.g] : [];
				memberBefore = [];
			} else if (contact) {
				form = formOf(contact);
				before = formOf(contact);
				if (agentDraft?.fields.id === contact.id) {
					form = contactDraftForm(form, agentDraft.fields);
					agentDraft = null;
					fromAgent = true;
				}
				member = groupsOf(groups, contact.id).map((g) => g.id);
				memberBefore = [...member];
			}
		});
	});
	// "Bitte einen Namen …" goes once the form names someone.
	$effect(() => {
		if (formError && formError === t('contacts.form.nobody') && namesSomeone(form)) formError = '';
	});
	const memberDirty = $derived(
		[...member].sort().join('\u0000') !== [...memberBefore].sort().join('\u0000'),
	);
	const dirty = $derived(
		editing &&
			editKey !== '' &&
			(photo.kind !== 'keep' ||
				(mode === 'edit' && memberDirty) ||
				JSON.stringify(form) !== JSON.stringify(before ?? blankForm())),
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
		leave(
			mode === 'edit' && selId
				? contactsHref({ ...listPlace, id: selId })
				: contactsHref(listPlace),
		);
	}

	/** Group memberships changed in the form: each group once, with its etag. */
	async function saveMembership(id: string) {
		const add = member.filter((g) => !memberBefore.includes(g));
		const remove = memberBefore.filter((g) => !member.includes(g));
		let next = groups;
		for (const gid of [...add, ...remove]) {
			const g = next.find((x) => x.id === gid);
			if (!g) continue;
			const done = await updateGroup(g, add.includes(gid) ? { add: [id] } : { remove: [id] });
			next = next.map((x) => (x.id === done.id ? done : x));
		}
		if (overview) overview = { ...overview, groups: next };
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
			const isNew = mode === 'new';
			let id: string;
			let etag: string;
			if (isNew) {
				const made = await createContact(inputOf(form, null));
				written(made);
				id = made.id;
				etag = made.etag;
			} else {
				if (!contact) return;
				id = contact.id;
				etag = contact.etag;
				const input = inputOf(form, before);
				if (Object.keys(input).length) {
					const saved = await updateContact(id, etag, input);
					written(saved);
					etag = saved.etag;
				}
			}
			let note = '';
			// The picture after the card, with the card's new etag.
			try {
				let done: Saved | null = null;
				if (photo.kind === 'file') done = await putPhotoFile(id, etag, photo.file);
				else if (photo.kind === 'nas') done = await putPhotoNas(id, etag, photo.root, photo.path);
				else if (photo.kind === 'remove') done = await removePhoto(id, etag);
				if (done) {
					written(done);
					pictures = { ...pictures, [id]: done.etag };
				}
			} catch (err) {
				note = t('contacts.form.photoFailed', { error: apiErrorText(err) });
			}
			try {
				if (memberDirty) await saveMembership(id);
			} catch (err) {
				note = t('contacts.groups.saveFailed', { error: apiErrorText(err) });
			}
			if (photo.kind === 'file') URL.revokeObjectURL(photo.preview);
			photo = { kind: 'keep' };
			memberBefore = [...member];
			contact = null;
			if (isNew) void refreshOverview();
			leave(contactsHref({ ...listPlace, id }));
			notice = note;
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
			const id = contact.id;
			await removeContact(id, contact.etag);
			askDelete = false;
			contact = null;
			dropRow(id);
			if (overview)
				overview = {
					...overview,
					groups: overview.groups.map((g) => ({
						...g,
						members: g.members.filter((m) => m !== id),
					})),
				};
			void refreshOverview();
			leave(contactsHref(listPlace));
		} catch (err) {
			delConflict = err instanceof ApiError && err.code === 'contacts_conflict';
			delError = delConflict ? t('contacts.conflict') : failed(err);
		} finally {
			busy = false;
		}
	}

	// ---- Groups: new, rename, delete, a contact dropped on one ----
	let groupDialog = $state<{ kind: 'new' | 'rename'; group?: Group; name: string } | null>(null);
	let groupDelete = $state<Group | null>(null);
	let groupError = $state('');
	function replaceGroup(g: Group) {
		if (overview)
			overview = { ...overview, groups: overview.groups.map((x) => (x.id === g.id ? g : x)) };
	}
	async function saveGroup() {
		if (!groupDialog) return;
		const name = groupDialog.name.trim();
		if (!name) {
			groupError = t('contacts.groups.nameMissing');
			return;
		}
		busy = true;
		groupError = '';
		try {
			if (groupDialog.kind === 'new') {
				const g = await createGroup(name);
				if (overview) overview = { ...overview, groups: [...overview.groups, g] };
				groupDialog = null;
				void refreshOverview();
				go({ g: g.id }, false);
			} else if (groupDialog.group) {
				replaceGroup(await updateGroup(groupDialog.group, { name }));
				groupDialog = null;
				void refreshOverview();
			}
		} catch (err) {
			groupError =
				err instanceof ApiError && err.code === 'contacts_conflict'
					? t('contacts.groups.conflict')
					: failed(err);
		} finally {
			busy = false;
		}
	}
	async function deleteGroup() {
		if (!groupDelete) return;
		busy = true;
		groupError = '';
		try {
			const id = groupDelete.id;
			await removeGroup(groupDelete);
			if (overview) overview = { ...overview, groups: overview.groups.filter((g) => g.id !== id) };
			groupDelete = null;
			if (place.g === id) go({}, false);
		} catch (err) {
			groupError =
				err instanceof ApiError && err.code === 'contacts_conflict'
					? t('contacts.groups.conflict')
					: failed(err);
			if (err instanceof ApiError && err.code === 'contacts_conflict') void refreshOverview();
		} finally {
			busy = false;
		}
	}
	async function dropOnGroup(g: Group, id: string) {
		notice = '';
		try {
			const done = await updateGroup(g, { add: [id] });
			replaceGroup(done);
			addedTo = t('contacts.groups.added', {
				name: nameOf(id),
				group: done.name || t('contacts.groups.unnamed'),
			});
		} catch (err) {
			notice = failed(err);
			if (err instanceof ApiError && err.code === 'contacts_conflict') void refreshOverview();
		}
	}
	async function removeFromGroup(g: Group, id: string) {
		try {
			replaceGroup(await updateGroup(g, { remove: [id] }));
		} catch (err) {
			notice = failed(err);
			if (err instanceof ApiError && err.code === 'contacts_conflict') void refreshOverview();
		}
	}
	// A group that is gone (deleted on the iPhone) leaves the address.
	$effect(() => {
		if (place.g && overview && !group) untrack(() => go({}, false));
	});

	// ---- Actions of a contact ----
	function writeMail(addr: string) {
		if (mail) openCompose({ a: '', to: addr, back: here });
		else window.location.href = 'mailto:' + addr;
	}
	async function newEvent() {
		if (!contact) return;
		const name = contact.name || contact.org;
		zone = await loadNasZone();
		const now = wallOf(new Date(), zone);
		const time = clock(Math.min(23 * 60, (Math.floor(now.minutes / 60) + 1) * 60));
		openEventWindow(eventHref({ date: now.date, time, title: name, back: here }));
	}
	function newContact() {
		go({ ...listPlace, isNew: true }, !wide);
	}
	function pick(to: SidePlace) {
		if (to.kind === 'book') go({}, false);
		else if (to.kind === 'group') go({ g: to.id }, false);
		else go({ view: to.kind }, false);
	}
	const sidePlace = $derived<SidePlace>(
		view === 'suggestions'
			? { kind: 'suggestions' }
			: view === 'duplicates'
				? { kind: 'duplicates' }
				: place.g
					? { kind: 'group', id: place.g }
					: { kind: 'book' },
	);
	/** The narrow window's "‹ Zurück" (the phone has the arrow of its bar). */
	function up() {
		leave(contactsUp(page.url));
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
		'contacts.new': { run: newContact, enabled: () => shown.kind === 'ready' },
		// 1.15: the KI-Agent in the program's ⋯ menu (menus.ts names the entry).
		'contacts.agent': {
			run: () => setAgent(!agentOpen),
			enabled: () => !!session.services.agent,
			checked: () => agentOpen,
		},
	});

	const listHeading = $derived(
		view === 'suggestions'
			? t('contacts.suggestions')
			: view === 'duplicates'
				? t('contacts.dup.title')
				: group
					? group.name || t('contacts.groups.unnamed')
					: bookName,
	);
	// The title bar names the place: the program, in a group or view its name.
	const title = $derived(
		mode === 'new'
			? t('contacts.newTitle')
			: mode === 'edit'
				? t('contacts.editTitle')
				: phone || (view === 'contacts' && !group)
					? t('desktop.app.contacts')
					: listHeading,
	);
	const frameLevel = $derived<FrameLevel>(level);
	const books = $derived(
		(overview?.books ?? []).map((b) =>
			overview && overview.books.length === 1 ? { ...b, count: all.length } : b,
		),
	);
	const duplicates = $derived(dups === null ? (overview?.duplicates ?? 0) : dupShown.length);
	const sheet = $derived(compact && editing);

	// ---- "Meine Karte" (1.15): a contact the person chose, per account ----
	// Kept on the NAS (preference kind "contacts", {"me": id}); shown only
	// when one is chosen and still in the book.
	const meId = $derived(session.contacts?.me ?? '');
	let meError = $state('');
	function setMe(id: string) {
		const before = session.contacts;
		session.contacts = id ? { me: id } : {};
		meError = '';
		api.put('/preferences/contacts', id ? { me: id } : {}).catch((err) => {
			session.contacts = before;
			meError = apiErrorText(err);
		});
	}
	const me = $derived(
		!group && view === 'contacts' && meId ? (all.find((i) => i.id === meId) ?? null) : null,
	);

	// ---- "Import und Export" (a row of "Mehr") ----
	let transfer = $state(false);

	// ---- The KI-Agent's panel (1.13) ----
	const AGENT_KEY = 'titanos.contacts.agentPanel';
	const withAgent = $derived(!!session.services.agent);
	let agentOpen = $state(readAgentOpen());
	/** The panel stands as a column (room next to the contact); else a sheet. */
	let agentColumn = $state(false);
	function readAgentOpen(): boolean {
		try {
			return localStorage.getItem(AGENT_KEY) === '1';
		} catch {
			return false;
		}
	}
	function setAgent(on: boolean) {
		agentOpen = on;
		try {
			if (on) localStorage.setItem(AGENT_KEY, '1');
			else localStorage.removeItem(AGENT_KEY);
		} catch {
			// Kept for this visit only.
		}
	}
	const openRow = $derived(selId && !place.isNew ? all.find((i) => i.id === selId) : undefined);
	const agentContext = $derived<AgentContext | null>(
		openRow ? { app: 'contacts', card: { book: openRow.book, id: openRow.id } } : null,
	);
	const agentActions = $derived(contactsPanelActions((k) => t(k)));
	/** The phone's ⋯ menu has the entry "KI-Agent" (desktop/menubar/menus.ts). */
	const menuHasAgent = $derived(
		entries(menusFor('app:contacts', session.data?.user?.role ?? 'user')).some(
			(e) => !!e.do && 'cmd' in e.do && e.do.cmd === 'contacts.agent',
		),
	);
	/** A ⋯ of the page's own: compact where no bar of the program shows the agent. */
	const ownMenu = $derived(compact && withAgent && !(phone && menuHasAgent));
</script>

<PageHead {title} />

{#snippet side()}
	<ContactSide
		{books}
		{groups}
		place={sidePlace}
		total={all.length}
		{mail}
		{duplicates}
		{wide}
		{compact}
		loading={overview === null && shown.kind === 'loading'}
		onpick={pick}
		onnewgroup={() => {
			groupError = '';
			groupDialog = { kind: 'new', name: '' };
		}}
		ondropgroup={dropOnGroup}
		ontransfer={() => (transfer = true)}
	/>
{/snippet}

{#snippet rail()}
	<ContactRail {groups} place={sidePlace} {mail} {duplicates} onpick={pick} />
{/snippet}

{#snippet agentToggle(folded: boolean)}
	{#if withAgent}
		<button
			type="button"
			class="side-link agent-toggle"
			class:icon={folded}
			class:agent-level={folded}
			aria-pressed={agentOpen}
			title={folded ? t('agentpim.panel.toggle') : undefined}
			aria-label={folded ? t('agentpim.panel.toggle') : undefined}
			onclick={() => setAgent(!agentOpen)}
			><Icon name="sparkle" size={17} />{#if !folded}<span>{t('agentpim.panel.toggle')}</span
				>{/if}</button
		>
	{/if}
{/snippet}

{#snippet foot(folded: boolean)}
	<div class="foot" class:folded>
		{@render agentToggle(folded)}
	</div>
{/snippet}

{#snippet notices()}
	{#if notice}<p class="notice-error notice" role="alert">{notice}</p>{/if}
	{#if draftNote}
		<p class="muted notice" role="status">
			{draftNote}
			<button type="button" class="btn small-btn" onclick={() => (draftNote = '')}
				>{t('common.close')}</button
			>
		</p>
	{/if}
	{#if addedTo}
		<p class="notice-ok notice" role="status">
			{addedTo}
			<button type="button" class="btn small-btn" onclick={() => (addedTo = '')}
				>{t('common.close')}</button
			>
		</p>
	{/if}
{/snippet}

{#snippet levelBack(label: string)}
	{#if compact}
		<div class="level-head">
			<button type="button" class="link back" onclick={up} data-back
				><Icon name="chevronLeft" size={22} />{label}</button
			>
		</div>
	{/if}
{/snippet}

{#snippet emptyBook()}
	<div class="empty-book" data-contacts-empty>
		<Icon name="person" size={36} />
		<p class="et">{t('contacts.empty.title')}</p>
		<p class="muted">{t('contacts.empty.text')}</p>
		<div class="ea">
			<button type="button" class="btn btn-primary" onclick={newContact}
				><Icon name="plus" size={15} />{t('contacts.new')}</button
			>
			<button type="button" class="btn" onclick={() => (importing = true)}
				><Icon name="upload" size={15} />{t('contacts.import')}</button
			>
			<a class="btn" href="/contacts/iphone"
				><Icon name="device" size={15} />{t('calendar.side.iphone')}</a
			>
		</div>
	</div>
{/snippet}

<!-- Next to "+": the group's menu and, where no bar shows it, the page's own ⋯ with the KI-Agent. -->
{#snippet listTools()}
	{#if group}
		{@const g = group}
		<Dropdown label={t('contacts.groups.menu', { name: listHeading })} icon="more" align="right">
			{#snippet children(close)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						groupError = '';
						groupDialog = { kind: 'rename', group: g, name: g.name };
					}}>{t('contacts.groups.rename')}</button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						groupError = '';
						groupDelete = g;
					}}>{t('contacts.groups.delete')}</button
				>
				{#if ownMenu}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							setAgent(true);
						}}>{t('agentpim.panel.toggle')}</button
					>
				{/if}
			{/snippet}
		</Dropdown>
	{:else if ownMenu}
		{@render ownMore()}
	{/if}
{/snippet}

{#snippet ownMore()}
	<Dropdown label={t('contacts.act.more')} icon="more" align="right">
		{#snippet children(close)}
			<button
				type="button"
				role="menuitem"
				class="agent-level"
				onclick={() => {
					close();
					setAgent(true);
				}}><Icon name="sparkle" size={16} />{t('agentpim.panel.toggle')}</button
			>
		{/snippet}
	</Dropdown>
{/snippet}

{#snippet stateNote()}
	{#if shown.kind === 'loading'}
		<p class="muted pad" role="status">{t('common.loading')}</p>
	{:else if shown.kind === 'unavailable'}
		<div class="unavail" role="status">
			<Icon name="disk" size={22} />
			<div>
				<p class="ut">{t('contacts.unavailable.title')}</p>
				<p>{t('contacts.unavailable.text')}</p>
			</div>
		</div>
	{:else if shown.kind === 'error'}
		<p class="notice-error pad" role="alert">{apiErrorText(shown.error)}</p>
	{/if}
{/snippet}

{#snippet listColumn()}
	<!-- The page's state for tests and the loading column (test-ui-pim-visibility.mjs). -->
	<div
		class="lc"
		data-pim-shell="contacts"
		data-pim-state={shown.kind}
		data-pim-ready={shown.kind === 'ready' ? '' : undefined}
		data-mode={mode}
		data-view={view}
	>
		{@render listBody()}
	</div>
{/snippet}

{#snippet listBody()}
	{#if shown.kind !== 'ready'}
		{#if compact}{@render levelBack(t('contacts.lists.title'))}{/if}
		{@render stateNote()}
	{:else if view === 'suggestions'}
		<div class="sugs" data-suggestions>
			{@render levelBack(t('contacts.lists.title'))}
			<h2 class="big">{t('contacts.sug.title')}</h2>
			{#if !mail}
				<p class="explain" data-suggestions-off>{t('contacts.sug.noMail')}</p>
			{:else}
				<label class="search">
					<Icon name="search" size={17} />
					<input
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
						phone={compact}
						busy={taking === s.addr}
						ontake={take}
					/>
				{/each}
			{/if}
		</div>
	{:else if view === 'duplicates'}
		<div class="dups" data-duplicates>
			{@render levelBack(t('contacts.lists.title'))}
			<h2 class="big">{t('contacts.dup.title')}</h2>
			<p class="muted small">{t('contacts.dup.text')}</p>
			{#if dupError}<p class="notice-error" role="alert">{dupError}</p>{/if}
			{#if dups === null && !dupError}
				<p class="muted" role="status">{t('common.loading')}</p>
			{:else if !dupShown.length}
				<p class="muted">{t('contacts.dup.none')}</p>
			{/if}
			{#each dupShown as s (setKey(s))}
				{@const current = openSet === s}
				<button
					type="button"
					class="dset"
					class:sel={current}
					aria-current={current ? 'true' : undefined}
					data-dup-set={s.ids[0]}
					onclick={() => go({ view: 'duplicates', set: s.ids[0] }, !wide)}
				>
					<b>{s.ids.map(nameOf).join(' · ')}</b>
					<small>{dupText(s)}</small>
				</button>
			{/each}
		</div>
	{:else}
		<ContactList
			{items}
			{total}
			{query}
			{answered}
			suggestions={sugHits}
			selected={wide ? selId : ''}
			{pictures}
			{today}
			{compact}
			{taking}
			heading={listHeading}
			tools={group || ownMenu ? listTools : undefined}
			onback={compact ? up : undefined}
			onnew={newContact}
			ready={shown.kind === 'ready'}
			{me}
			empty={!all.length && !group ? emptyBook : undefined}
			drag={wide}
			{onquery}
			onopen={(item) => go({ ...listPlace, id: item.id }, !wide)}
			ontake={take}
		/>
	{/if}
{/snippet}

{#snippet editorHead()}
	<div class="ed-head" class:sheet-head={sheet}>
		<button type="button" class="link" disabled={busy} onclick={cancel} data-editor-cancel
			>{t('common.cancel')}</button
		>
		<h2>{mode === 'new' ? t('contacts.newTitle') : t('contacts.editTitle')}</h2>
		<button
			type="button"
			class="link strong"
			disabled={busy || shown.kind !== 'ready' || editKey === ''}
			onclick={save}
			data-editor-save>{t('contacts.form.done')}</button
		>
	</div>
{/snippet}

{#snippet editor()}
	{@render editorHead()}
	<div class="ed-body">
		{#if fromAgent}<p class="notice-warn msg" role="note" data-agent-draft>
				{t('agentpim.draft.contactNote')}
			</p>{/if}
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
				bind:member
				contact={mode === 'new' ? null : contact}
				phone={compact}
				{busy}
				{groups}
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
	</div>
{/snippet}

<ColumnFrame
	storage="titanos.contacts"
	columns={wide}
	level={frameLevel}
	class={wide ? 'contacts wide' : 'contacts levels'}
	sideClass="col-side"
	listClass="col-list"
	contentClass="col-contact"
	sideWidth={{ min: 180, max: 400, fallback: 240 }}
	listWidth={{ min: 240, max: 600, fallback: 330 }}
	contentMin={300}
	foldBelow={900}
	sideResizeLabel={t('contacts.resize.side')}
	listResizeLabel={t('contacts.resize.list')}
	{side}
	{rail}
	sideFoot={withAgent ? foot : undefined}
	notices={notice || addedTo || draftNote ? notices : undefined}
	list={listColumn}
	aside={withAgent && agentOpen && wide ? agentAside : undefined}
	asideKey="titanos.agentPanel.width"
	asideResizeLabel={t('agentpim.panel.resize')}
	bind:asideShown={agentColumn}
>
	<div class="content" data-content-mode={mode}>
		{#if shown.kind !== 'ready'}
			<!-- Loading and "nicht verfügbar" stand in the list's column. -->
		{:else if editing}
			{#if sheet}
				<!-- Compact: Apple's sheet over the screen, the list dimmed behind it. -->
				<div class="dim" aria-hidden="true"></div>
				<div
					class="sheet"
					class:phone
					role="dialog"
					aria-modal="true"
					aria-label={title}
					data-editor-sheet
				>
					{@render editor()}
				</div>
			{:else}
				{@render editor()}
			{/if}
		{:else if mode === 'show'}
			{#if contactError}
				{@render levelBack(listHeading)}
				<p class="notice-error" role="alert">{contactError}</p>
			{:else if contact && contact.id === selId}
				{@const c = contact}
				{#if meError}<p class="notice-error" role="alert">{meError}</p>{/if}
				<ContactDetail
					contact={c}
					soon={soonOf(c.id)}
					{today}
					{compact}
					{mail}
					book={bookName}
					groups={contactGroups}
					backLabel={listHeading}
					onback={up}
					isMe={meId === c.id}
					inGroup={group && group.members.includes(c.id) ? listHeading : ''}
					onedit={() => go({ ...listPlace, id: selId, edit: true }, !wide)}
					onmail={writeMail}
					onevent={() => void newEvent()}
					onme={(on) => setMe(on ? c.id : '')}
					onleavegroup={() => group && void removeFromGroup(group, c.id)}
				/>
			{:else}
				{@render levelBack(listHeading)}
				<p class="muted" role="status">{t('common.loading')}</p>
			{/if}
		{:else if view === 'duplicates' && openSet}
			{@const s = openSet}
			{@render levelBack(t('contacts.dup.title'))}
			<MergeView
				set={s}
				{nameOf}
				{today}
				phone={compact}
				onmerged={merged}
				onhide={() => hideSet(s)}
			/>
		{:else if view === 'suggestions'}
			{#if mail}<p class="muted none">{t('contacts.sug.about')}</p>{/if}
		{:else if view === 'duplicates'}
			<p class="muted none">{dupShown.length ? t('contacts.dup.pick') : t('contacts.dup.none')}</p>
		{:else if all.length}
			<p class="muted none">{t('contacts.pick')}</p>
		{/if}
	</div>
</ColumnFrame>

{#snippet agentAside()}
	<AgentPanel
		app="contacts"
		context={agentContext}
		actions={agentActions}
		onclose={() => setAgent(false)}
	/>
{/snippet}

{#if withAgent && agentOpen && !(wide && agentColumn)}
	<AgentSheet label={t('agentpim.panel.title')} onclose={() => setAgent(false)}>
		<AgentPanel
			app="contacts"
			phone
			context={agentContext}
			actions={agentActions}
			onclose={() => setAgent(false)}
		/>
	</AgentSheet>
{/if}

{#if transfer}
	<Dialog title={t('contacts.side.transfer')} onclose={() => (transfer = false)}>
		<p class="lead">{t('contacts.transfer.text')}</p>
		<div class="dialog-actions transfer">
			<button
				type="button"
				class="btn"
				data-transfer-export
				onclick={() => {
					transfer = false;
					void runExport();
				}}><Icon name="download" size={15} />{t('contacts.export')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				data-transfer-import
				onclick={() => {
					transfer = false;
					importing = true;
				}}><Icon name="upload" size={15} />{t('contacts.transfer.import')}</button
			>
		</div>
	</Dialog>
{/if}

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

{#if groupDialog}
	<Dialog
		title={groupDialog.kind === 'new' ? t('contacts.groups.new') : t('contacts.groups.rename')}
		locked={busy}
		onclose={() => (groupDialog = null)}
	>
		<form
			class="gform"
			onsubmit={(e) => {
				e.preventDefault();
				void saveGroup();
			}}
		>
			<label
				><span>{t('contacts.groups.name')}</span><input
					class="field"
					autocomplete="off"
					maxlength="200"
					bind:value={groupDialog.name}
					data-group-name
				/></label
			>
			{#if groupDialog.kind === 'new'}<p class="muted small">{t('contacts.groups.newText')}</p>{/if}
			{#if groupError}<p class="notice-error" role="alert">{groupError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (groupDialog = null)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={busy} data-group-save
					>{groupDialog.kind === 'new' ? t('contacts.groups.create') : t('common.save')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if groupDelete}
	<Dialog
		title={t('contacts.groups.delTitle', {
			name: groupDelete.name || t('contacts.groups.unnamed'),
		})}
		locked={busy}
		onclose={() => (groupDelete = null)}
	>
		<p class="lead">
			{t('contacts.groups.delText', { name: groupDelete.name || t('contacts.groups.unnamed') })}
		</p>
		{#if groupError}<p class="notice-error" role="alert">{groupError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (groupDelete = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={deleteGroup}
				data-group-delete><Icon name="trash" size={15} />{t('contacts.groups.delConfirm')}</button
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
	<ImportDialog onclose={() => (importing = false)} onchanged={() => void refresh()} />
{/if}

<style>
	.foot {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.foot.folded {
		align-items: center;
	}
	.side-link {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border-radius: calc(9px * var(--radius-scale, 1));
		color: var(--text);
		text-decoration: none;
	}
	.side-link.icon {
		justify-content: center;
		width: 44px;
		padding: 0;
	}
	button.side-link {
		width: 100%;
		border: 0;
		background: none;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	button.side-link.icon {
		width: 44px;
	}
	.agent-toggle[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.side-link :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.side-link span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.side-link:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.side-link:focus-visible,
	.link:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	/* The page's own ⋯ and the group's menu: an icon in the accent colour, 44 px. */
	:global(.contacts [data-contact-list] .navr .dropdown > .btn) {
		min-width: 44px;
		min-height: 44px;
		border: 0;
		background: none;
		color: var(--accent);
	}
	:global(.contacts [data-contact-list] .navr .dropdown > .btn .chevron) {
		display: none;
	}
	[role='menuitem'].agent-level {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.link {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 2px;
		min-width: 44px;
		min-height: 44px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	:global(.contacts.levels.compact) .link {
		font-size: 1.0625rem;
	}
	.link.back {
		padding-left: 0;
		text-align: left;
	}
	.link.strong {
		font-weight: 600;
	}
	.link:disabled {
		color: var(--muted);
		cursor: default;
	}
	.level-head {
		display: flex;
		align-items: center;
		min-height: 44px;
		margin: 0 calc(-1 * var(--space-2));
	}
	.big {
		margin: 0;
		font-size: 1.75rem;
		font-weight: 700;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.notice {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin: var(--space-2) var(--space-3);
		overflow-wrap: anywhere;
	}
	.small-btn {
		min-height: 32px;
	}
	.lc {
		display: flex;
		flex-direction: column;
		min-height: 100%;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-height: 100%;
		padding: var(--space-3) var(--space-4) var(--space-5);
	}
	:global(.contacts.levels.compact) .content {
		padding: 0 var(--space-3) var(--space-5);
	}
	.pad {
		margin: var(--space-3);
	}
	.unavail {
		display: flex;
		gap: var(--space-3);
		align-items: flex-start;
		margin: var(--space-3);
		padding: var(--space-4);
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-ctl, 10px);
		background: var(--warn-bg);
		color: var(--warn-text);
	}
	.unavail :global(svg) {
		flex: none;
	}
	.unavail p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.ut {
		font-weight: 600;
		margin-bottom: var(--space-1) !important;
	}
	.none {
		margin: var(--space-4) 0;
		text-align: center;
		overflow-wrap: break-word;
	}
	.msg {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin: 0 0 var(--space-3);
	}
	.lead {
		margin: 0;
		overflow-wrap: break-word;
	}
	.transfer {
		flex-wrap: wrap;
	}
	.transfer .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	/* The form's head: Abbrechen, the title, Fertig – sticky in its column. */
	.ed-head {
		position: sticky;
		top: calc(-1 * var(--space-3));
		z-index: 3;
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
		align-items: center;
		gap: var(--space-2);
		margin: calc(-1 * var(--space-3)) calc(-1 * var(--space-4)) 0;
		padding: var(--space-1) var(--space-2);
		background: var(--bg);
	}
	.ed-head .link:first-child {
		justify-self: start;
	}
	.ed-head .link:last-child {
		justify-self: end;
	}
	.ed-head h2 {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
		text-align: center;
		overflow-wrap: anywhere;
	}
	/* Compact: Apple's sheet – over the dimmed list, rounded on top. */
	.dim {
		position: fixed;
		inset: 0;
		z-index: 59;
		background: var(--overlay);
	}
	.sheet {
		position: fixed;
		inset: var(--space-3) 0 0;
		z-index: 60;
		overflow-y: auto;
		overscroll-behavior: contain;
		border-radius: calc(14px * var(--radius-scale, 1)) calc(14px * var(--radius-scale, 1)) 0 0;
		background: var(--bg);
		padding: 0 var(--space-4) calc(var(--space-5) + env(safe-area-inset-bottom, 0px));
	}
	/* On the phone the program's bar stays in sight above the sheet. */
	.sheet.phone {
		top: calc(52px + var(--space-3));
	}
	.ed-head.sheet-head {
		top: 0;
		margin: 0 calc(-1 * var(--space-4)) var(--space-2);
		padding: var(--space-1-5) var(--space-2);
		border-radius: inherit;
	}
	:root[data-fx-transitions='on'] .sheet {
		animation: sheet-up 0.22s ease-out;
	}
	:root[data-fx-transitions='on'] .dim {
		animation: dim-in 0.22s ease-out;
	}
	@keyframes sheet-up {
		from {
			transform: translateY(24px);
			opacity: 0.6;
		}
	}
	@keyframes dim-in {
		from {
			opacity: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		:root[data-fx-transitions='on'] .sheet,
		:root[data-fx-transitions='on'] .dim {
			animation: none;
		}
	}
	.ed-body {
		display: flex;
		flex-direction: column;
		padding-top: var(--space-2);
	}
	.sugs,
	.dups {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
	}
	:global(.contacts.levels.compact) .sugs,
	:global(.contacts.levels.compact) .dups {
		padding-top: 0;
	}
	.sugs p,
	.dups p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.explain {
		line-height: 1.5;
	}
	.search {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 44px;
		padding: 0 var(--space-2-5);
		border-radius: calc(10px * var(--radius-scale, 1));
		background: var(--chip);
		color: var(--muted);
	}
	.search input {
		flex: 1;
		min-width: 0;
		min-height: 40px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
	}
	.search input:focus {
		outline: none;
	}
	.search:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.small {
		font-size: var(--text-sm);
	}
	.dset {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
		min-height: 52px;
		padding: var(--space-2);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.dset:hover {
		background: var(--srow-hover);
	}
	.dset.sel {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.dset:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.dset small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.empty-book {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		margin: var(--space-5) var(--space-2);
		text-align: center;
		color: var(--muted);
	}
	.empty-book p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.et {
		color: var(--text);
		font-weight: 600;
		font-size: var(--text-md);
	}
	.ea {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}
	.ea .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		text-decoration: none;
	}
	.gform {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.gform label {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.gform p {
		margin: 0;
		overflow-wrap: break-word;
	}
	/* The levels (compact): one at a time, edge to edge, no cards. */
	:global(.contacts.levels) {
		display: flex;
		flex-direction: column;
	}
	:global(.contacts.levels .col-side) {
		padding: 0 0 var(--space-3);
	}
	@media (pointer: coarse) {
		.side-link {
			min-height: 44px;
		}
	}
</style>
