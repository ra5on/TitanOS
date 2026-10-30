import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	cardContrast,
	cleanLoginText,
	composite,
	defaultLoginLook,
	dimmed,
	isDefaultLook,
	loginAppearance,
	loginBackgroundURL,
	loginSaveBody,
	loginTexts,
	sanitizeLoginLook,
} from '../src/lib/login/look.ts';
import { defaultAppearance, resolvePalette, THEMES } from '../src/lib/theme/themes.ts';

const image = { id: 'b'.repeat(64), width: 1920, height: 1080, bytes: 1000 };

test('no settings or default settings keep today’s login page', () => {
	const inherited = { ...defaultAppearance(), theme: 'mitternacht' };
	assert.deepEqual(loginAppearance(null, inherited), { appearance: inherited, scoped: false });
	assert.equal(loginAppearance(defaultLoginLook(), inherited).scoped, false);
	assert.equal(isDefaultLook(sanitizeLoginLook({})), true);
	assert.equal(isDefaultLook(sanitizeLoginLook({ clock: true })), false);
});

test('a fixed theme starts from the defaults, inherit keeps the browser look', () => {
	const inherited = { ...defaultAppearance(), theme: 'schwarz', font: 120 };
	const fixed = loginAppearance(
		{ ...defaultLoginLook(), theme: 'hell', accent: 'rose', style: 'flat' },
		inherited,
	);
	assert.equal(fixed.scoped, true);
	assert.equal(fixed.appearance.theme, 'hell');
	assert.equal(fixed.appearance.accent, 'rose');
	assert.equal(fixed.appearance.style, 'flat');
	assert.equal(fixed.appearance.font, 100);
	const motif = loginAppearance(
		{
			...defaultLoginLook(),
			background: { ...defaultLoginLook().background, source: 'builtin', id: 'wellen' },
		},
		inherited,
	);
	assert.equal(motif.appearance.theme, 'schwarz');
	// A picture behind glass keeps the readable minimum opacity.
	assert.equal(motif.appearance.bg, 'image');
});

test('untrusted values never reach the page', () => {
	const look = sanitizeLoginLook({
		theme: 'eigen',
		accent: 'red',
		customAccent: 'red;display:none',
		glass: 999,
		blur: -5,
		background: { source: 'builtin', id: '../../api/v1/users', zoom: 900, dim: 99, fit: 'fill' },
		position: 'top',
		width: 'x',
		texts: { de: { title: 'a\nb<script>', notice: 'x'.repeat(400) }, fr: { title: 'Bonjour' } },
		hideHost: 'yes',
	});
	assert.equal(look.theme, 'inherit');
	assert.equal(look.accent, 'blau');
	assert.equal(look.customAccent, '#E5B92E');
	assert.equal(look.glass, 90);
	assert.equal(look.blur, 0);
	assert.equal(look.background.source, 'theme');
	assert.equal(look.background.id, '');
	assert.equal(look.background.zoom, 200);
	assert.equal(look.background.dim, 80);
	assert.equal(look.background.fit, 'cover');
	assert.equal(look.position, 'center');
	assert.equal(look.width, 'normal');
	assert.equal(look.texts.de.title, 'a b<script>');
	assert.equal([...look.texts.de.notice].length, 300);
	assert.equal(look.texts.fr, undefined);
	assert.equal(look.hideHost, false);
	assert.equal(loginBackgroundURL(look), '');
	// An own image needs stored metadata from titand.
	assert.equal(sanitizeLoginLook({ background: { source: 'custom' } }).background.source, 'theme');
	assert.equal(
		sanitizeLoginLook({ background: { source: 'custom' }, image: { id: 'x' } }).background.source,
		'theme',
	);
	assert.equal(cleanLoginText('  Hallo Welt  ', 60), 'Hallo Welt');
});

test('background URLs only point to the gallery or the configured image', () => {
	const motif = sanitizeLoginLook({ background: { source: 'builtin', id: 'nordlicht' } });
	assert.equal(loginBackgroundURL(motif), '/backgrounds/nordlicht.webp');
	const own = sanitizeLoginLook({ background: { source: 'custom' }, image });
	assert.equal(loginBackgroundURL(own), '/api/v1/login-appearance/background/' + image.id);
	assert.equal(loginBackgroundURL(own, 'blob:x'), 'blob:x');
	assert.equal(loginBackgroundURL(null), '');
});

test('texts fall back to the other language, then to the default title', () => {
	const look = sanitizeLoginLook({
		texts: { en: { title: 'Welcome', notice: 'Family only' }, de: { notice: 'Nur Familie' } },
	});
	assert.deepEqual(loginTexts(look, 'de', 'Anmelden'), { title: 'Welcome', notice: 'Nur Familie' });
	assert.deepEqual(loginTexts(look, 'en', 'Sign in'), { title: 'Welcome', notice: 'Family only' });
	assert.deepEqual(loginTexts(null, 'de', 'Anmelden'), { title: 'Anmelden', notice: '' });
});

test('the save body carries no image metadata or unknown fields', () => {
	const body = loginSaveBody(
		{ ...sanitizeLoginLook({ background: { source: 'custom' }, image }), configured: true },
		true,
	);
	assert.equal('image' in body, false);
	assert.equal('configured' in body, false);
	assert.equal(body.removeImage, true);
	assert.equal('id' in body.background, false);
	assert.equal(
		loginSaveBody(sanitizeLoginLook({ background: { source: 'builtin', id: 'kueste' } })).background
			.id,
		'kueste',
	);
});

test('card contrast follows surface style, glass opacity and darkening', () => {
	assert.equal(composite('#FFFFFF', 0.5, '#000000'), '#808080');
	assert.equal(dimmed('#FFFFFF', 100), '#000000');
	const palette = THEMES.graphit;
	const flat = { ...defaultAppearance(), style: 'flat', bg: 'image' };
	const glass = { ...defaultAppearance(), style: 'glass', glass: 90, bg: 'image' };
	const outline = { ...defaultAppearance(), style: 'outline', bg: 'image' };
	// A bright picture: flat stays readable, see-through cards do not.
	assert.ok(cardContrast(palette, flat, '#F5F5F5') >= 4.5);
	assert.ok(cardContrast(palette, outline, '#F5F5F5') < 4.5);
	assert.ok(cardContrast(palette, glass, '#F5F5F5') < cardContrast(palette, flat, '#F5F5F5'));
	// Darkening a bright picture makes an outline card readable again.
	assert.ok(cardContrast(palette, outline, dimmed('#F5F5F5', 80)) >= 4.5);
	// The light theme on its own background is fine.
	const hell = { ...defaultAppearance(), theme: 'hell', style: 'flat' };
	assert.ok(cardContrast(resolvePalette(hell, false), hell, THEMES.hell.bg) >= 4.5);
});
