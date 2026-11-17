// 1.05: the levels of T-Mail in the address – /mail (mailboxes and
// folders), /mail?a=…&f=… (a folder's list), /mail?a=…&f=…&m=… (a message).
// On the phone and in a narrow window one level shows at a time; the arrow
// of the phone's bar and a swipe from the edge go one level up.

/** What is read of an address (URL, or SvelteKit's read-only page.url). */
export type Address = { pathname: string; searchParams: { get(name: string): string | null } };

export type MailPlace = { a: string; f: string; m: number | null };

export function placeOf(url: Address): MailPlace {
	const m = Number(url.searchParams.get('m'));
	return {
		a: url.searchParams.get('a') ?? '',
		f: url.searchParams.get('f') ?? '',
		m: Number.isInteger(m) && m > 0 ? m : null,
	};
}

export function mailHref(p: Partial<MailPlace>): string {
	const q = new URLSearchParams();
	if (p.a) q.set('a', p.a);
	if (p.a && p.f) q.set('f', p.f);
	if (p.a && p.f && p.m) q.set('m', String(p.m));
	const s = q.toString();
	return '/mail' + (s ? '?' + s : '');
}

export type Level = 'folders' | 'list' | 'message';

export function levelOf(p: MailPlace): Level {
	if (p.a && p.f && p.m) return 'message';
	if (p.a && p.f) return 'list';
	return 'folders';
}

/** One level up from a page of T-Mail (the phone's arrow). */
export function mailUp(url: Address): string {
	const path = url.pathname.replace(/\/$/, '');
	if (path !== '/mail') {
		// Writing, setup and settings go back to the place they came from (or the start).
		const back = url.searchParams.get('back');
		return back && back.startsWith('/mail') && !back.startsWith('//') ? back : '/mail';
	}
	const p = placeOf(url);
	const level = levelOf(p);
	if (level === 'message') return mailHref({ a: p.a, f: p.f });
	if (level === 'list') return '/mail';
	return '/';
}
