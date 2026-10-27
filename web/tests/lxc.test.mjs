import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	containerLabel,
	defaultImage,
	groupImages,
	newContainerIdentifier,
	validMAC,
	validMountTarget,
} from '../src/lib/lxc.ts';
import { pages, palettePages } from '../src/lib/command-palette.ts';
import { readFileSync } from 'node:fs';

const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url), 'utf8'));
const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url), 'utf8'));

test('container labels map to safe identifiers', () => {
	assert.equal(newContainerIdentifier('Pi-hole DNS', []), 'pi-hole-dns');
	assert.equal(newContainerIdentifier('Pi-hole DNS', ['pi-hole-dns']), 'pi-hole-dns-2');
	assert.equal(newContainerIdentifier('日本語', []), 'ct');
	assert.match(newContainerIdentifier('x'.repeat(80), []), /^[a-z0-9][a-z0-9-]{0,30}$/);
	assert.equal(containerLabel({ name: 'web' }), 'web');
	assert.equal(containerLabel({ name: 'web', displayName: 'Web server' }), 'Web server');
});

test('mount targets follow the rules of the backend', () => {
	for (const ok of ['/mnt/media', '/srv/data', '/home/alex/files', '/var/lib/app', '/etc/app'])
		assert.equal(validMountTarget(ok), true, ok);
	for (const bad of [
		'',
		'/',
		'mnt/x',
		'/mnt/a b',
		'/mnt/../etc',
		'/mnt/./x',
		'/etc',
		'/usr',
		'/proc/x',
		'/sys/fs',
		'/dev/x',
		'/run/x',
		'/mnt/x/',
		'/mnt//x',
	])
		assert.equal(validMountTarget(bad), false, bad);
});

test('MAC addresses must be unicast', () => {
	assert.equal(validMAC(''), true);
	assert.equal(validMAC('00:16:3E:AA:BB:CC'), true);
	assert.equal(validMAC('01:16:3e:aa:bb:cc'), false);
	assert.equal(validMAC('00:00:00:00:00:00'), false);
	assert.equal(validMAC('00:16:3e:aa:bb'), false);
});

test('images are grouped with Debian as the default', () => {
	const images = [
		{ distribution: 'ubuntu', release: 'jammy', arch: 'amd64', build: '1' },
		{ distribution: 'alpine', release: '3.9', arch: 'amd64', build: '1' },
		{ distribution: 'alpine', release: '3.22', arch: 'amd64', build: '1' },
		{ distribution: 'debian', release: 'bookworm', arch: 'amd64', build: '1' },
		{ distribution: 'debian', release: 'trixie', arch: 'amd64', build: '1' },
	];
	assert.deepEqual(groupImages(images)[0], { distribution: 'alpine', releases: ['3.22', '3.9'] });
	assert.deepEqual(defaultImage(images), { distribution: 'debian', release: 'trixie' });
	assert.deepEqual(defaultImage(images.slice(0, 3)), { distribution: 'alpine', release: '3.22' });
	assert.deepEqual(defaultImage([]), { distribution: '', release: '' });
});

test('the container page is in the palette for administrators only, with texts in both languages', () => {
	assert.ok(pages.some((page) => page.href === '/lxc' && page.admin));
	const translate = (key) => key;
	assert.ok(palettePages('owner', translate).some((entry) => entry.href === '/lxc'));
	assert.ok(!palettePages('user', translate).some((entry) => entry.href === '/lxc'));
	for (const key of Object.keys(de).filter(
		(key) => key.startsWith('lxc.') || key.startsWith('error.lxc_') || key === 'nav.lxc',
	))
		assert.ok(en[key], key);
	assert.equal(de['nav.lxc'], 'Container');
});
