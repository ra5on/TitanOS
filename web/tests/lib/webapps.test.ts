// PLAN 66: web apps in the browser (docs/WEBAPPS.md) – the messages of
// host.js, where a save goes, standard apps per file type, the file API.
import { afterEach, expect, test, vi } from 'vitest';
import {
	acceptTypes,
	appSrc,
	appsFor,
	catalogDefault,
	chosenApp,
	cleanName,
	copyName,
	EDITOR,
	extOf,
	hostMessage,
	knownTypes,
	letterGradient,
	mtimeNs,
	ownAppUrl,
	probeFiles,
	parentDir,
	sanitizeFileApps,
	savedMtime,
	saveTarget,
	validFileName,
	type WebApp,
} from '../../src/lib/webapps/protocol.ts';
import { readDoc, writeDoc } from '../../src/lib/webapps/files.ts';
import { ApiError } from '../../src/lib/api.ts';

const app = (id: string, types: string[], o: Partial<WebApp> = {}): WebApp => ({
	id,
	name: id[0].toUpperCase() + id.slice(1),
	version: '1.0.0',
	installed: true,
	state: 'ready',
	bridge: 'host',
	types,
	default: false,
	url: `/webapps/${id}/1.0.0/`,
	...o,
});
const APPS = [
	app('wordcraft', ['docx', 'doc', 'odt', 'rtf'], { default: true }),
	app('pdfcraft', ['pdf'], { default: true }),
	app('vectorcraft', ['svg', 'ai'], { bridge: 'adapter' }),
	app('deckcraft', ['pptx']),
	app('gridcraft', ['xlsx', 'csv'], { default: true, installed: false, state: 'missing' }),
];

test('host messages: only well-formed ones of the contract pass', () => {
	expect(hostMessage({ type: 'webapp-ready' })).toEqual({ type: 'webapp-ready' });
	const bytes = new ArrayBuffer(4);
	expect(hostMessage({ type: 'webapp-save', name: '../../a.docx', saveAs: 1, bytes })).toEqual({
		type: 'webapp-save',
		name: 'a.docx',
		saveAs: false,
		bytes,
	});
	expect(hostMessage({ type: 'webapp-save', name: 'a', bytes: new Uint8Array(3) })).toBeNull();
	expect(hostMessage({ type: 'webapp-save', name: 'a', bytes: 'text' })).toBeNull();
	expect(hostMessage({ type: 'webapp-dirty', dirty: 'yes' })).toEqual({
		type: 'webapp-dirty',
		dirty: false,
	});
	expect(hostMessage({ type: 'webapp-failed', text: 'x'.repeat(900) })).toEqual({
		type: 'webapp-failed',
		text: 'x'.repeat(500),
	});
	for (const bad of [
		null,
		'webapp-ready',
		3,
		{},
		{ type: 'webapp-open' },
		{ type: 'titanos-page' },
	])
		expect(hostMessage(bad)).toBeNull();
});

test('file names from the app are cleaned like the Craft contract does', () => {
	expect(cleanName('C:\\Users\\sam\\notes.pdf', 'x')).toBe('notes.pdf');
	expect(cleanName('a\u0000b\nc.txt', 'x')).toBe('abc.txt');
	expect(cleanName('..', 'doc')).toBe('doc');
	expect(cleanName('dir/', 'doc')).toBe('doc');
	expect(cleanName(42, 'doc')).toBe('doc');
	expect(Array.from(cleanName('é'.repeat(300), 'x'))).toHaveLength(255);
});

test('save decision: the original only for its own format', () => {
	const doc = { root: 'share/Documents', path: 'Angebot.docx' };
	const word = { bridge: 'host' as const, types: ['docx', 'doc', 'odt', 'rtf'] };
	expect(saveTarget(doc, { name: 'Angebot.docx', saveAs: false }, word)).toBe('original');
	expect(saveTarget(doc, { name: 'angebot.DOCX', saveAs: false }, word)).toBe('original');
	// An export (PDF) never goes over the original: a notice offers to keep it.
	expect(saveTarget(doc, { name: 'Angebot.pdf', saveAs: false }, word)).toBe('created');
	// The document in another format the app edits (a .doc saved as .docx,
	// GridCraft's .csv as .xlsx): asked where, then it is the window's file.
	expect(
		saveTarget({ root: 'r', path: 'alt.doc' }, { name: 'alt.docx', saveAs: false }, word),
	).toBe('dialog');
	// Another file of the same format (a backup copy, the next export) is
	// not the open document: never over it (PLAN 76).
	expect(saveTarget(doc, { name: 'Angebot (Sicherung).docx', saveAs: false }, word)).toBe(
		'created',
	);
	// The app keeps calling the document by the name it had before the
	// person chose another one in the save dialog.
	expect(saveTarget(doc, { name: 'Unbenannt.docx', saveAs: false }, word, 'Unbenannt.docx')).toBe(
		'original',
	);
	expect(saveTarget(doc, { name: 'Angebot.docx', saveAs: true }, word)).toBe('dialog');
	expect(saveTarget(null, { name: 'Neu.docx', saveAs: false }, word)).toBe('dialog');
	// Without the bridge every save asks where (the app only downloads).
	expect(
		saveTarget(doc, { name: 'Angebot.docx', saveAs: false }, { bridge: 'adapter', types: [] }),
	).toBe('dialog');
});

test('modification times in nanoseconds, exact', () => {
	expect(mtimeNs('2026-11-07T09:12:03.123456789+01:00')).toBe('1794039123123456789');
	expect(mtimeNs('2026-11-07T08:12:03.123456789Z')).toBe('1794039123123456789');
	expect(mtimeNs('2026-11-07T08:12:03Z')).toBe('1794039123000000000');
	expect(mtimeNs('2026-11-07T08:12:03.5Z')).toBe('1794039123500000000');
	expect(mtimeNs('2026-11-07T03:42:03.000000001-04:30')).toBe('1794039123000000001');
	expect(mtimeNs('1970-01-01T00:00:00Z')).toBe('0');
	for (const bad of ['', 'gestern', '2026-11-07', '2026-11-07T08:12:03'])
		expect(mtimeNs(bad)).toBeNull();
	// The answer of PUT: modified first, else mtime (text or safe number).
	expect(savedMtime({ modified: '2026-11-07T08:12:03.5Z', mtime: '1' })).toBe(
		'1794039123500000000',
	);
	expect(savedMtime({ mtime: '1794039123500000000' })).toBe('1794039123500000000');
	expect(savedMtime({ mtime: 1794039123500000000 })).toBeNull();
	expect(savedMtime({ mtime: 'abc' })).toBeNull();
	expect(savedMtime(null)).toBeNull();
});

test('names and paths', () => {
	const taken = new Set(['Angebot (Kopie).docx']);
	expect(copyName('Angebot.docx', (n) => taken.has(n), 'Kopie')).toBe('Angebot (Kopie 2).docx');
	expect(copyName('README', () => false, 'copy')).toBe('README (copy)');
	expect(extOf('a/b/Bericht.Final.PDF')).toBe('pdf');
	expect(extOf('.env')).toBe('');
	expect(parentDir('a/b/c.docx')).toBe('a/b');
	expect(parentDir('c.docx')).toBe('');
	expect(validFileName(' Brief.docx ')).toBe(true);
	for (const bad of ['', '..', 'a/b', 'a\\b']) expect(validFileName(bad)).toBe(false);
});

test('standard apps: own choice, catalog default, TitanOS for what it shows itself', () => {
	expect(
		sanitizeFileApps({ docx: 'webapp:wordcraft', txt: EDITOR, pdf: 'x', 'B!': 'editor' }),
	).toEqual({ docx: 'webapp:wordcraft', txt: 'editor' });
	expect(sanitizeFileApps(['x'])).toEqual({});
	expect(appsFor('docx', APPS).map((a) => a.id)).toEqual(['wordcraft']);
	expect(appsFor('xlsx', APPS)).toEqual([], 'GridCraft is not installed');
	expect(catalogDefault('pdf', APPS, true)?.id).toBe('pdfcraft', 'basic set wins over the preview');
	expect(catalogDefault('svg', APPS, true)).toBeNull();
	expect(catalogDefault('pptx', APPS, false)?.id).toBe('deckcraft');
	expect(chosenApp('docx', APPS, {})).toBe('webapp:wordcraft');
	expect(chosenApp('docx', APPS, { docx: EDITOR })).toBe(EDITOR);
	expect(chosenApp('svg', APPS, { svg: 'webapp:vectorcraft' }, true)).toBe('webapp:vectorcraft');
	// A chosen app that is gone falls back to the default.
	expect(chosenApp('docx', APPS, { docx: 'webapp:gone' })).toBe('webapp:wordcraft');
	expect(chosenApp('png', APPS, {}, true)).toBeNull();
	expect(knownTypes(APPS)).toEqual(['ai', 'doc', 'docx', 'odt', 'pdf', 'pptx', 'rtf', 'svg']);
});

test('the frame address and icons', () => {
	expect(appSrc(APPS[0], 'Alex Muster')).toBe(
		'/webapps/wordcraft/1.0.0/?author=Alex+Muster&bridge=host&app=wordcraft',
	);
	expect(appSrc({ ...APPS[2], url: '/webapps/vectorcraft/1' }, 'a')).toBe(
		'/webapps/vectorcraft/1/?author=a&bridge=adapter&app=vectorcraft',
	);
	expect(ownAppUrl('/webapps/wordcraft/0.16.0/')).toBe(true);
	for (const bad of ['https://evil.example/', '/api/v1/files', '/webapps/../x/', '//webapps/a/b/'])
		expect(ownAppUrl(bad)).toBe(false);
	expect(letterGradient('wordcraft')).toBe('blue');
	expect(letterGradient('somecraft')).toBe(letterGradient('somecraft'));
});

// --- The file API (fetch stubbed) --------------------------------------------

afterEach(() => vi.unstubAllGlobals());
function stubFetch(handler: (url: string, init?: RequestInit) => Response) {
	const calls: { url: string; init?: RequestInit }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init?: RequestInit) => {
			calls.push({ url, init });
			return handler(url, init);
		}),
	);
	return calls;
}
const json = (body: unknown, status = 200) =>
	new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

test('writeDoc: PUT with root, path and expect; 409 is a conflict; the time comes back', async () => {
	const calls = stubFetch(() =>
		json({ modified: '2026-11-07T10:00:00.5+01:00', mtime: '1', size: 3 }),
	);
	const doc = { root: 'share/Documents', path: 'Ordner/Angebot.docx' };
	const r = await writeDoc(doc, new Uint8Array([1, 2, 3]).buffer, '1794039123123456789');
	expect(r).toEqual({ ok: true, mtime: '1794042000500000000' });
	const url = new URL(calls[0].url, 'http://x');
	expect(url.pathname).toBe('/api/v1/files/content');
	expect(Object.fromEntries(url.searchParams)).toEqual({
		root: 'share/Documents',
		path: 'Ordner/Angebot.docx',
		expect: '1794039123123456789',
	});
	expect(calls[0].init?.method).toBe('PUT');

	stubFetch(() => json({ error: { code: 'file_changed', message: 'changed' } }, 409));
	expect(await writeDoc(doc, new ArrayBuffer(1), 'none')).toEqual({ ok: false, conflict: true });

	stubFetch(() => json({ error: { code: 'no_space', message: 'full' } }, 507));
	await expect(writeDoc(doc, new ArrayBuffer(1), 'none')).rejects.toBeInstanceOf(ApiError);
});

test('readDoc: the time is taken before the bytes', async () => {
	const order: string[] = [];
	stubFetch((url) => {
		const path = new URL(url, 'http://x').pathname;
		order.push(path);
		if (path.endsWith('/properties')) return json({ modified: '2026-11-07T08:12:03Z' });
		return new Response(new Uint8Array([80, 75, 3, 4]));
	});
	const r = await readDoc({ root: 'share/D', path: 'a.docx' });
	expect(order).toEqual(['/api/v1/files/properties', '/api/v1/files/download']);
	expect(r.mtime).toBe('1794039123000000000');
	expect(new Uint8Array(r.bytes)).toEqual(new Uint8Array([80, 75, 3, 4]));
});

// PLAN 75-18: Cloudflare Access turns cookie-less requests away with a redirect.
test('probeFiles tells a blocked app from a reachable one', async () => {
	const answer = (type: string, status: number) => async () => ({ type, status }) as Response;
	const url = '/webapps/pdfcraft/1.0.0/';
	expect(await probeFiles(url, answer('basic', 200))).toBe(true);
	expect(await probeFiles(url, answer('opaqueredirect', 0))).toBe(false);
	expect(await probeFiles(url, answer('basic', 302))).toBe(false);
	expect(await probeFiles(url, answer('basic', 403))).toBe(false);
	expect(
		await probeFiles(url, async () => {
			throw new TypeError('offline');
		}),
	).toBe(true);
	// The request carries no cookie and follows no redirect.
	const seen: RequestInit[] = [];
	await probeFiles(url, (async (_p: string, init: RequestInit) => {
		seen.push(init);
		return { type: 'basic', status: 200 } as Response;
	}) as typeof fetch);
	expect(seen.every((i) => i.credentials === 'omit' && i.redirect === 'manual')).toBe(true);
});

test('acceptTypes reads the file picker of the app (PLAN 75-20)', () => {
	expect(acceptTypes('.docx,.DOC')).toEqual(['docx', 'doc']);
	expect(acceptTypes('application/pdf')).toEqual(['pdf']);
	expect(acceptTypes('image/*')).toContain('png');
	expect(acceptTypes('image/jpeg')).toEqual(['jpg', 'jpeg']);
	// Not understood, or none: every file.
	expect(acceptTypes('')).toEqual([]);
	expect(acceptTypes('weird stuff')).toEqual([]);
	expect(hostMessage({ type: 'webapp-pick', accept: '.xlsx', multiple: true })).toEqual({
		type: 'webapp-pick',
		types: ['xlsx'],
		multiple: true,
	});
});
