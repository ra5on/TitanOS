// PLAN 86: the search of the control panel finds every single setting. The
// index is generated from the settings pages (scripts/settings-index.mjs);
// these tests keep it fresh, check it against a plain scan of the pages and
// check what the owner looked for and did not find before.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { output, missedTranslation, OUTPUT } from '../scripts/settings-index.mjs';
import { settingsAreas } from '../src/lib/settings-search.ts';
import { findSettings, findHash, otherPages, settingsHitHref } from '../src/lib/settings-find.ts';
import { findTitles } from '../src/lib/settings-jump.ts';
import { settingsIndex } from '../src/lib/settings-index.gen.ts';

const i18n = new URL('../src/lib/i18n/', import.meta.url);
function messages(locale) {
	const all = JSON.parse(readFileSync(new URL(`${locale}.json`, i18n), 'utf8'));
	for (const f of readdirSync(new URL('features/', i18n)))
		if (f.endsWith(`.${locale}.json`))
			Object.assign(all, JSON.parse(readFileSync(new URL(`features/${f}`, i18n), 'utf8')));
	return all;
}
const de = messages('de');
const en = messages('en');
const tDe = (key) => de[key] ?? key;
const tEn = (key) => en[key] ?? key;

const generated = await output(settingsAreas, otherPages);

test('the generated index is up to date (run npm run settings-index after changing a page)', () => {
	assert.equal(readFileSync(OUTPUT, 'utf8'), generated.text);
});

test('every title the generator could not read is data, not a forgotten setting', () => {
	const missed = generated.unresolved.filter((u) =>
		missedTranslation(u.split(': ').slice(1).join(': ')),
	);
	assert.deepEqual(missed, []);
	// The classifier: a plain translation is a setting, names and values are data.
	assert.ok(missedTranslation("t('look.mode')"));
	assert.ok(missedTranslation('t(`energy.${item.key}`)'));
	assert.ok(!missedTranslation('peer.name'));
	assert.ok(!missedTranslation("t('cluster.nodesOf', { name })"));
});

test('a plain scan of the pages finds no translated row title the index lacks', () => {
	// Independent of the parser: every literal t('key') title of a group, row
	// or part in a file the index reads must be in the index – unless the row
	// only leads to another page (href) or stands in a dialog.
	const keys = new Set(settingsIndex.flatMap((e) => [e.k, ...(e.in ?? [])]).filter(Boolean));
	const missing = [];
	for (const file of generated.files) {
		const text = readFileSync(file, 'utf8');
		const dialog = /<Dialog\b/.test(text);
		for (const m of text.matchAll(/<Setting(?:Row|Group|Sub)\b/g)) {
			const tag = text.slice(m.index, m.index + 700).split(/\n\s*(?:<\/?[A-Za-z]|\{[#:/@])/)[0];
			const title = /\btitle=\{t\('([\w.]+)'\)\}/.exec(tag)?.[1];
			if (!title || /\bhref=/.test(tag) || keys.has(title)) continue;
			// Rows of a dialog are not on the page until it opens.
			if (dialog && text.lastIndexOf('<Dialog', m.index) > text.lastIndexOf('</Dialog>', m.index))
				continue;
			missing.push(`${relative(fileURLToPath(new URL('..', import.meta.url)), file)}: ${title}`);
		}
	}
	assert.deepEqual(missing, []);
});

test('every key of the index has a text in both languages', () => {
	for (const e of settingsIndex)
		for (const key of [e.k, ...(e.in ?? []).filter((r) => !r.startsWith('='))].filter(Boolean)) {
			assert.ok(de[key], `de ${key}`);
			assert.ok(en[key], `en ${key}`);
		}
	// Every settings page of the menu has rows in the index – but Benutzer, a
	// list of accounts (the page itself is found by its name and keywords).
	for (const area of settingsAreas.filter((a) => a.id !== 'users'))
		assert.ok(
			settingsIndex.some(
				(e) => e.p === `/settings/${area.id}` || e.p.startsWith(`/settings/${area.id}/`),
			),
			area.id,
		);
});

const first = (q, admin = true, t = tDe) => findSettings(q, admin, t, settingsIndex)[0];
const hrefs = (q, admin = true, t = tDe) =>
	findSettings(q, admin, t, settingsIndex).map((h) => h.href);

test('what the owner looked for is found, best hit first (German)', () => {
	assert.equal(first('Cluster').href, '/settings/cluster');
	assert.ok(hrefs('Knoten').includes('/settings/cluster'));
	assert.equal(first('mehrere NAS').href, '/settings/cluster');
	assert.equal(
		first('Sprache').href,
		'/settings/appearance?tab=colors#find=Sprache%20und%20Schrift|Sprache',
	);
	assert.equal(first('Sprache').where, 'Darstellung › Farben und Schrift › Sprache und Schrift');
	assert.equal(
		first('Schriftgröße').href,
		'/settings/appearance?tab=colors#find=Sprache%20und%20Schrift|Schriftgr%C3%B6%C3%9Fe',
	);
	assert.equal(first('Tailscale').href, '/settings/network?tab=remote#remote-tailscale');
	assert.equal(first('Akzent').label, 'Akzentfarbe');
	assert.ok(hrefs('Kernel').includes('/settings/updates#find=Automatik|Kernel'));
	assert.ok(hrefs('Glocke').some((h) => h.startsWith('/account#')));
	for (const q of ['eingeloggt', 'dauerhaft', 'abmelden'])
		assert.ok(hrefs(q).includes('/settings/access#access-stay'), q);
	assert.ok(hrefs('sperren').includes('/settings/access#access-idle-lock'));
	assert.equal(first('ssh').href, '/settings/access#access-ssh');
	assert.ok(hrefs('Zeitzone').includes('/settings/timezone'));
	assert.ok(hrefs('platten-ruhe').includes('/settings/energy'));
	// A part inside a row: the titles lead through the row.
	assert.ok(
		hrefs('Exit Node').some((h) =>
			h.startsWith('/settings/network?tab=remote#find=Fernzugriff|Tailscale|'),
		),
	);
	assert.deepEqual(hrefs('xyzzy'), []);
});

test('the same in English', () => {
	assert.equal(first('Language', true, tEn).label, 'Language');
	assert.match(first('Language', true, tEn).href, /^\/settings\/appearance\?tab=colors#find=/);
	assert.equal(first('Font size', true, tEn).label, 'Font size');
	assert.equal(first('Cluster', true, tEn).href, '/settings/cluster');
	assert.ok(hrefs('nodes', true, tEn).includes('/settings/cluster'));
	assert.equal(first('Tailscale', true, tEn).href, '/settings/network?tab=remote#remote-tailscale');
	assert.ok(hrefs('stay logged in', true, tEn).includes('/settings/access#access-stay'));
});

test('an account with the role "user" finds only what it may open', () => {
	const user = (q) => hrefs(q, false);
	assert.deepEqual(
		user('ssh').filter((h) => h.startsWith('/settings')),
		[],
	);
	assert.deepEqual(user('Cluster'), []);
	assert.equal(first('Sprache', false).label, 'Sprache');
	// The login page tab of Darstellung is for owners and administrators.
	assert.ok(!user('Anmeldeseite').some((h) => h.includes('tab=login')));
	assert.ok(hrefs('Anmeldeseite').some((h) => h.includes('tab=login')));
	// Mein Konto: a user's own phone row, an admin's bell.
	assert.ok(user('Mitteilungen auf diesem Gerät').some((h) => h.startsWith('/account')));
	assert.ok(!hrefs('Mitteilungen auf diesem Gerät').some((h) => h.startsWith('/account')));
});

test('a hit leads to its tab and row; the anchor survives the trip', () => {
	const entry = {
		p: '/settings/appearance',
		k: 'appearance.fontSize',
		kind: 'row',
		tab: 'colors',
		in: ['look.langText'],
	};
	const href = settingsHitHref(entry, tDe);
	const url = new URL(href, 'http://nas');
	assert.equal(url.searchParams.get('tab'), 'colors');
	assert.deepEqual(findTitles(url.hash), ['Sprache und Schrift', 'Schriftgröße']);
	assert.deepEqual(findTitles('#' + findHash(['A | B', 'C, d'])), ['A | B', 'C, d']);
	assert.equal(findTitles('#access-ssh'), null);
	assert.equal(
		settingsHitHref({ p: '/settings/network', kind: 'tab', tab: 'remote' }, tDe),
		'/settings/network?tab=remote',
	);
	assert.equal(
		settingsHitHref(
			{ p: '/settings/access', k: 'stay.title', kind: 'row', id: 'access-stay' },
			tDe,
		),
		'/settings/access#access-stay',
	);
	assert.equal(
		settingsHitHref({ p: '/settings/x', x: 'Tailscale', kind: 'row', in: ['=Fern'] }, tDe),
		'/settings/x#find=Fern|Tailscale',
	);
});
