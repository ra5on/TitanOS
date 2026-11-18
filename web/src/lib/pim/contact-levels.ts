// 1.13 (owner 18 Nov 2026): the places of Kontakte in the address, like
// Mail's levels (lib/tmail/levels.ts). On the desktop the columns show
// side bar, list and contact at once; on the phone and in a narrow window
// one level at a time – the side bar (books, groups, `?side=1`), the list
// (all contacts, a group `?g=`, the suggestions `?view=suggestions`, the
// duplicates `?view=duplicates`) and the content (a contact `?id=`, its
// form `&edit=1`, a new one `?new=1`, a set of duplicates `&set=`). The
// phone's arrow goes one level up (PhoneBar).
import type { Address } from '../tmail/levels.ts';

export type ContactView = 'contacts' | 'suggestions' | 'duplicates';
export type ContactPlace = {
	view: ContactView;
	/** A group (its file name); '' for the whole book. */
	g: string;
	/** The open contact. */
	id: string;
	edit: boolean;
	isNew: boolean;
	/** The side bar as a level of its own (compact). */
	side: boolean;
	/** The open set of duplicates (its first contact). */
	set: string;
};
export type ContactLevel = 'side' | 'list' | 'content';

export function contactPlaceOf(url: Address): ContactPlace {
	const q = url.searchParams;
	const v = q.get('view');
	const view: ContactView = v === 'suggestions' || v === 'duplicates' ? v : 'contacts';
	return {
		view,
		g: view === 'contacts' ? (q.get('g') ?? '') : '',
		id: q.get('id') ?? '',
		edit: q.get('edit') === '1',
		isNew: q.get('new') === '1',
		side: q.get('side') === '1',
		set: view === 'duplicates' ? (q.get('set') ?? '') : '',
	};
}

export function contactsHref(p: Partial<ContactPlace>): string {
	const q = new URLSearchParams();
	if (p.side) q.set('side', '1');
	if (p.view && p.view !== 'contacts') q.set('view', p.view);
	if (p.g && (!p.view || p.view === 'contacts')) q.set('g', p.g);
	if (p.set && p.view === 'duplicates') q.set('set', p.set);
	if (p.isNew) q.set('new', '1');
	if (p.id) q.set('id', p.id);
	if (p.id && p.edit) q.set('edit', '1');
	const s = q.toString();
	return '/contacts' + (s ? '?' + s : '');
}

export function contactLevelOf(p: ContactPlace): ContactLevel {
	if (p.side) return 'side';
	if (p.isNew || p.id || p.set) return 'content';
	return 'list';
}

/** One level up (the phone's arrow, the narrow window's "‹ Zurück"). */
export function contactsUp(url: Address): string {
	const path = url.pathname.replace(/\/$/, '');
	if (path !== '/contacts') return '/contacts';
	const p = contactPlaceOf(url);
	if (p.id && p.edit) return contactsHref({ view: p.view, g: p.g, id: p.id });
	const level = contactLevelOf(p);
	if (level === 'content') return contactsHref({ view: p.view, g: p.g });
	if (level === 'list') return contactsHref({ side: true, view: p.view, g: p.g });
	return '/';
}
