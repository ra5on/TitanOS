// 1.11, Kontakte (Sicherheitsdurchsicht S-15): the web address of a card
// shows the target it opens – a look-alike letter or a backslash in the
// card must not make the shown host differ from the real one. Notes and
// numbers link only http(s)/mailto/tel, with rel="noopener noreferrer".
import { expect, test } from 'vitest';
import { render } from '@testing-library/svelte';
import ContactDetail from '#lib/pim/con/ContactDetail.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import type { Contact } from '#lib/pim/contacts.ts';

i18n.locale = 'de';

const contact = (over: Partial<Contact>): Contact => ({
	id: 'a.vcf',
	book: 'kontakte',
	etag: 'e1',
	first: 'Anna',
	last: 'Beispiel',
	name: 'Anna Beispiel',
	org: '',
	phones: [],
	emails: [],
	addresses: [],
	birthday: '',
	notes: '',
	hasPhoto: false,
	url: '',
	modified: '',
	...over,
});

function show(c: Contact) {
	const noop = () => {};
	return render(ContactDetail, {
		props: {
			contact: c,
			soon: null,
			today: '2026-11-18',
			phone: false,
			mail: false,
			book: 'Kontakte',
			onedit: noop,
			onmail: noop,
			onevent: noop,
		},
	});
}

test('the web address shows the target it opens', () => {
	for (const [url, target] of [
		['https://аpple.com', 'https://xn--pple-43d.com/'],
		['https://evil.example\\.good.example', 'https://evil.example/.good.example'],
	]) {
		const { container, unmount } = show(contact({ url }));
		const links = [...container.querySelectorAll('a[target="_blank"]')];
		expect(links).toHaveLength(1);
		expect(links[0].getAttribute('href')).toBe(target);
		expect(links[0].textContent).toBe(target);
		expect(links[0].getAttribute('rel')).toBe('noopener noreferrer');
		unmount();
	}
});

test('notes link only http(s), mailto and tel; numbers become tel: links', () => {
	const { container } = show(
		contact({
			notes: 'Seite https://example.org/a, Mail mailto:anna@example.org, javascript:alert(1)',
			phones: [{ type: 'mobile', label: '', value: 'javascript:alert(1)' }],
		}),
	);
	const hrefs = [...container.querySelectorAll('a')].map((a) => a.getAttribute('href') ?? '');
	expect(hrefs).toContain('https://example.org/a');
	expect(hrefs).toContain('mailto:anna@example.org');
	expect(hrefs.some((h) => /^javascript:/i.test(h))).toBe(false);
	expect(hrefs).toContain('tel:1');
});
