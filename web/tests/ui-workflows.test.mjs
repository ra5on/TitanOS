import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	sanitizeShareView,
	visibleShares,
	shareNavigation,
	shareFilesURL,
} from '../src/lib/share-view.ts';
import { sanitizeDockerLayout, dockerGroups, stackPrimary } from '../src/lib/docker-layout.ts';
import { palettePages, findPaletteEntries } from '../src/lib/command-palette.ts';

test('share preferences and filters accept only supported bounded values', () => {
	assert.deepEqual(
		sanitizeShareView({
			volume: '../\nsecret',
			protocol: 'shell',
			sort: 'path',
			descending: 1,
			compact: 'yes',
		}),
		sanitizeShareView(null),
	);
	const shares = [
		{ name: 'Media10', volume: 'Data', smb: true, nfs: { enabled: false } },
		{ name: 'Media2', volume: 'Data', smb: true, nfs: { enabled: true } },
		{ name: 'Photos', volume: 'Fast', smb: false, nfs: { enabled: true } },
	];
	const view = sanitizeShareView({ volume: 'Data', protocol: 'nfs' });
	assert.deepEqual(
		visibleShares(shares, view, 'media', 'en').map((s) => s.name),
		['Media2'],
	);
	assert.deepEqual(
		visibleShares(shares, sanitizeShareView(null), '', 'en').map((s) => s.name),
		['Media2', 'Media10', 'Photos'],
	);
	assert.equal(
		new URL(shareFilesURL('Private & Team'), 'https://nas.test').searchParams.get('root'),
		'share/Private & Team',
	);
});
test('share navigation does not expose another account’s search or selection', () => {
	shareNavigation('alice').query = 'private';
	shareNavigation('alice').selected = 'Personal';
	assert.equal(shareNavigation('alice').query, 'private');
	assert.deepEqual(shareNavigation('bob'), { account: 'bob', query: '', selected: '', scroll: 0 });
	assert.equal(shareNavigation('alice').query, '');
});
test('stack main service survives recreation and missing choices never fall back', () => {
	const items = [
		{ name: 'photos-web-old', app: 'photos', service: 'web', id: 'old', webui: '2283' },
		{ name: 'photos-db', app: 'photos', service: 'db', id: 'db', webui: '', publicUrl: '' },
	];
	const group = dockerGroups(items)[0];
	assert.equal(stackPrimary(group, {}).service, 'web');
	const saved = sanitizeDockerLayout({
		primaryServices: { 'app:photos': 'web', 'not-an-id': 'db', 'app:bad': 'bad\nname' },
	});
	assert.deepEqual(saved.primaryServices, { 'app:photos': 'web' });
	const recreated = dockerGroups([{ ...items[0], id: 'new', name: 'photos-web-new' }, items[1]])[0];
	assert.equal(stackPrimary(recreated, saved.primaryServices).id, 'new');
	assert.equal(stackPrimary(dockerGroups([items[1]])[0], saved.primaryServices), undefined);
	assert.equal(stackPrimary(group, { 'app:photos': 'db' }).service, 'db');
});
test('palette omits privileged destinations for users and searches words without accents', () => {
	const translate = (k) => k;
	const user = palettePages('user', translate),
		admin = palettePages('owner', translate);
	assert.equal(
		user.some((e) => e.href === '/docker'),
		false,
	);
	assert.equal(
		user.some((e) => e.href.startsWith('/settings/access')),
		false,
	);
	assert.equal(
		admin.some((e) => e.href === '/settings/access#access-ssh'),
		true,
	);
	assert.deepEqual(
		findPaletteEntries(
			[{ label: 'Übertragung prüfen', section: 'Werkzeuge', href: '/tools' }],
			'ubertragung prufen',
		).map((e) => e.href),
		['/tools'],
	);
	assert.equal(
		findPaletteEntries(
			Array.from({ length: 100 }, (_, i) => ({ label: String(i), section: '', href: '/a' + i })),
			'',
		).length,
		60,
	);
});
