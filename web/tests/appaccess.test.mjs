import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('app links go through TitanOS so that the app gate lets the browser in', async () => {
	const { gatedAppURL } = await import('../src/lib/app-links.ts');
	const open = (path) => '/api/v1' + path;
	assert.equal(
		gatedAppURL('immich', 'https://immich.nas.local', open),
		'/api/v1/apps/immich/open?host=immich.nas.local',
	);
	assert.equal(
		gatedAppURL('immich', 'https://immich.nas.local:8443/', open),
		'/api/v1/apps/immich/open?host=immich.nas.local',
	);
	// No app or no valid address: as before.
	assert.equal(gatedAppURL('', 'https://x.nas.local', open), 'https://x.nas.local/');
	assert.equal(gatedAppURL('immich', 'javascript:alert(1)', open), '');
	assert.equal(gatedAppURL('immich', '', open), '');
});

// PLAN 75-20: Einstellungen › Programme is gone; the search leads to Benutzer.
test('the app rights are found under Benutzer, Programme is no settings area any more', async () => {
	const { settingsAreas, matchesSetting } = await import('../src/lib/settings-search.ts');
	assert.equal(
		settingsAreas.find((a) => a.id === 'programs'),
		undefined,
	);
	for (const q of ['Programme', 'rechte', 'apps', 'benutzer'])
		assert.ok(matchesSetting(q, 'Benutzer', 'users'), q);
	assert.ok(matchesSetting('zertifizierungsstelle', 'HTTPS-Namen für Apps', 'proxy'));
});

test('the texts of app access exist in German and English with the same keys', () => {
	const read = (lang) =>
		JSON.parse(
			readFileSync(
				new URL(`../src/lib/i18n/features/appaccess.${lang}.json`, import.meta.url),
				'utf8',
			),
		);
	const de = read('de');
	const en = read('en');
	assert.deepEqual(Object.keys(en).sort(), Object.keys(de).sort());
	for (const key of Object.keys(de)) {
		assert.ok(key.startsWith('appaccess.'), key);
		assert.ok(de[key].trim() && en[key].trim(), key);
	}
});
