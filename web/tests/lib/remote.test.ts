// PLAN 63: the protocol of the remote view (docs/FERNANSICHT.md).
import { expect, test } from 'vitest';
import {
	chunkRanges,
	cursorCss,
	fitRect,
	inputMessages,
	isDesktopShortcut,
	isHalfKey,
	macOptionChar,
	fitText,
	MAX_TEXT_MESSAGE,
	isPaste,
	isPrintable,
	keyMessage,
	loadView,
	mapPoint,
	missingSupport,
	parseAudio,
	parseMessage,
	parseVideo,
	sanitizeImage,
	sanitizeSettings,
	sanitizeStatus,
	saveView,
	sizeMessage,
	UPLOAD_CHUNK,
	uploadChunk,
	videoMessage,
	viewerError,
	wheelDelta,
} from '#lib/desktop/remote.ts';

const key = (k: string, code: string, mods: Partial<Record<string, boolean>> = {}) => ({
	key: k,
	code,
	shiftKey: !!mods.shift,
	ctrlKey: !!mods.ctrl,
	altKey: !!mods.alt,
	metaKey: !!mods.meta,
});

test('video header: type, key flag, 64-bit big-endian microseconds, Annex-B payload', () => {
	const au = new Uint8Array([0, 0, 0, 1, 0x67, 0x42, 0, 0, 1, 0x65, 0x88]);
	const ts = 2 ** 33 + 123456;
	const msg = videoMessage(true, ts, au);
	const bytes = new Uint8Array(msg);
	expect(bytes[0]).toBe(1);
	expect(bytes[1]).toBe(1);
	expect([...bytes.slice(2, 10)]).toEqual([0, 0, 0, 2, 0, 1, 0xe2, 0x40]);
	const v = parseVideo(msg)!;
	expect(v.key).toBe(true);
	expect(v.timestamp).toBe(ts);
	expect([...v.data]).toEqual([...au]);
	expect(parseVideo(videoMessage(false, 5, au))!.key).toBe(false);
	// Other flag bits do not make a key frame.
	const odd = new Uint8Array(videoMessage(false, 5, au));
	odd[1] = 0b110;
	expect(parseVideo(odd.buffer)!.key).toBe(false);
	expect(parseVideo(new Uint8Array([1, 1, 0]).buffer)).toBeNull();
	expect(parseVideo(new Uint8Array([2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9]).buffer)).toBeNull();
});

test('audio header: type, microseconds, one Opus packet', () => {
	const buf = new Uint8Array([2, 0, 0, 0, 0, 0, 0, 0x4e, 0x20, 0xfc, 0xff, 0xfe]).buffer;
	const a = parseAudio(buf)!;
	expect(a.timestamp).toBe(20000);
	expect([...a.data]).toEqual([0xfc, 0xff, 0xfe]);
	expect(parseAudio(new Uint8Array([2, 0, 0]).buffer)).toBeNull();
	expect(parseAudio(new Uint8Array([1, 0, 0, 0, 0, 0, 0, 0, 0, 1]).buffer)).toBeNull();
});

test('uploads: 0x03, 32-bit id, at most 256 KiB per piece, ranges cover the file', () => {
	const c = new Uint8Array(uploadChunk(0x01020304, new Uint8Array([9, 8])));
	expect([...c]).toEqual([3, 1, 2, 3, 4, 9, 8]);
	expect(() => uploadChunk(1, new Uint8Array(UPLOAD_CHUNK + 1))).toThrow();
	expect(chunkRanges(0)).toEqual([]);
	expect(chunkRanges(10)).toEqual([[0, 10]]);
	const size = UPLOAD_CHUNK * 2 + 5;
	const r = chunkRanges(size);
	expect(r).toEqual([
		[0, UPLOAD_CHUNK],
		[UPLOAD_CHUNK, UPLOAD_CHUNK * 2],
		[UPLOAD_CHUNK * 2, size],
	]);
});

test('text messages: known ones parsed and checked, everything else ignored', () => {
	expect(
		parseMessage(
			'{"t":"hello","codec":"avc1.42E01F","width":1280,"height":800,"gpu":"vaapi","audio":true}',
		),
	).toEqual({
		t: 'hello',
		codec: 'avc1.42E01F',
		width: 1280,
		height: 800,
		gpu: 'vaapi',
		audio: true,
	});
	expect(parseMessage('{"t":"hello","codec":"x","width":0,"height":800}')).toBeNull();
	expect(
		parseMessage('{"t":"hello","codec":"x","width":10,"height":8,"gpu":"evil"}'),
	).toMatchObject({
		gpu: 'cpu',
		audio: false,
	});
	expect(parseMessage('{"t":"stats","fps":59,"kbps":4200,"latencyMs":18}')).toEqual({
		t: 'stats',
		fps: 59,
		kbps: 4200,
		latencyMs: 18,
	});
	expect(parseMessage('{"t":"bye","reason":"weird"}')).toEqual({
		t: 'bye',
		reason: 'error',
		text: '',
	});
	expect(parseMessage('{"t":"download","name":"a.jpg","size":3,"id":"d1"}')).toMatchObject({
		id: 'd1',
	});
	expect(parseMessage('{"t":"download","name":"a.jpg"}')).toBeNull();
	expect(parseMessage('not json')).toBeNull();
	expect(parseMessage('{"t":"nope"}')).toBeNull();
	expect(parseMessage('null')).toBeNull();
});

test('refusals: codes of titand from error messages and close reasons', () => {
	expect(viewerError('viewer_memory')).toBe('viewer_memory');
	expect(viewerError('app_forbidden')).toBe('app_forbidden');
	expect(viewerError('viewer_not_running')).toBe('viewer_not_running');
	expect(viewerError('')).toBeNull();
	expect(viewerError('other')).toBeNull();
	expect(parseMessage('{"t":"starting"}')).toEqual({ t: 'starting' });
	expect(
		parseMessage(
			'{"t":"error","code":"viewer_image","text":"x","image":{"state":"downloading","bytes":5,"size":10,"progress":0.5}}',
		),
	).toEqual({
		t: 'error',
		code: 'viewer_image',
		text: 'x',
		image: { state: 'downloading', version: '', bytes: 5, size: 10, progress: 0.5, error: '' },
	});
	expect(parseMessage('{"t":"bye","reason":"replaced"}')).toMatchObject({ reason: 'replaced' });
	expect(parseMessage('{"t":"bye","reason":"forbidden"}')).toMatchObject({ reason: 'forbidden' });
});

test('cursor shapes only from a list of CSS cursors', () => {
	expect(cursorCss('pointer')).toBe('pointer');
	expect(cursorCss('url(evil), auto')).toBe('default');
});

test('keys: desktop shortcuts stay, printable vs. function keys, paste', () => {
	expect(isDesktopShortcut(key('q', 'KeyQ', { alt: true }))).toBe(true);
	expect(isDesktopShortcut(key('W', 'KeyW', { alt: true, shift: true }))).toBe(true);
	expect(isDesktopShortcut(key('ArrowLeft', 'ArrowLeft', { alt: true }))).toBe(true);
	expect(isDesktopShortcut(key(' ', 'Space', { ctrl: true }))).toBe(true);
	expect(isDesktopShortcut(key(' ', 'Space', { meta: true }))).toBe(true);
	expect(isDesktopShortcut(key('q', 'KeyQ'))).toBe(false);
	expect(isDesktopShortcut(key('@', 'KeyQ', { ctrl: true, alt: true }))).toBe(false);
	expect(isDesktopShortcut(key('ArrowLeft', 'ArrowLeft'))).toBe(false);

	expect(isPrintable(key('ä', 'Quote'))).toBe(true);
	expect(isPrintable(key('Enter', 'Enter'))).toBe(false);
	expect(isPrintable(key('a', 'KeyA', { ctrl: true }))).toBe(false);
	expect(isPaste(key('v', 'KeyV', { ctrl: true }))).toBe(true);
	expect(isPaste(key('v', 'KeyV', { meta: true }))).toBe(true);
	expect(isPaste(key('v', 'KeyV'))).toBe(false);

	expect(keyMessage(key('A', 'KeyA', { shift: true }), true)).toEqual({
		t: 'key',
		code: 'KeyA',
		key: 'A',
		down: true,
		mods: { shift: true, ctrl: false, alt: false, meta: false },
	});
});

test('dead keys and IME halves stay home, Mac Option characters lose Alt', () => {
	expect(isHalfKey(key('Dead', 'Equal'))).toBe(true);
	expect(isHalfKey({ ...key('e', 'KeyE'), isComposing: true })).toBe(true);
	expect(isHalfKey(key('Process', 'KeyA'))).toBe(true);
	expect(isHalfKey(key('é', 'KeyE'))).toBe(false);
	// AltGr (Ctrl+Alt) is handled by the agent; Option+L on a German Mac types @.
	expect(macOptionChar(key('@', 'KeyL', { alt: true }), true)).toBe(true);
	expect(macOptionChar(key('@', 'KeyL', { alt: true }), false)).toBe(false);
	expect(macOptionChar(key('ArrowLeft', 'ArrowLeft', { alt: true }), true)).toBe(false);
	expect(macOptionChar(key('c', 'KeyC', { alt: true, meta: true }), true)).toBe(false);
});

test('text and clipboard are cut to what titand accepts', () => {
	const bytes = (m: object) => new TextEncoder().encode(JSON.stringify(m)).length;
	expect(fitText('clipboard', 'Hallo')).toEqual({ t: 'clipboard', text: 'Hallo' });
	const big = fitText('clipboard', 'ä'.repeat(800_000));
	expect(bytes(big)).toBeLessThanOrEqual(MAX_TEXT_MESSAGE);
	expect(big.text.length).toBeGreaterThan(500_000);
	const quotes = fitText('text', '"'.repeat(700_000));
	expect(bytes(quotes)).toBeLessThanOrEqual(MAX_TEXT_MESSAGE);
});

test('phone keyboard: text, backspace and enter from input events', () => {
	expect(inputMessages('insertText', 'Hallo')).toEqual([{ t: 'text', text: 'Hallo' }]);
	expect(inputMessages('insertCompositionText', 'é')).toEqual([{ t: 'text', text: 'é' }]);
	const back = inputMessages('deleteContentBackward', null);
	expect(back.map((m) => ('down' in m ? [m.code, m.down] : m))).toEqual([
		['Backspace', true],
		['Backspace', false],
	]);
	expect(inputMessages('insertLineBreak', null)[0]).toMatchObject({ code: 'Enter', down: true });
	expect(inputMessages('historyUndo', null)).toEqual([]);
});

test('pointer: letterboxed picture, mapped to remote pixels and clamped', () => {
	// 1280×800 in a 1000×1000 box: full width, 625 high, centred.
	const box = fitRect(1000, 1000, 1280, 800);
	expect(box).toEqual({ left: 0, top: 187.5, width: 1000, height: 625 });
	const shown = { ...box, left: 100 + box.left, top: 50 + box.top };
	expect(mapPoint(100, 50 + 187.5, shown, 1280, 800)).toEqual({ x: 0, y: 0 });
	expect(mapPoint(600, 50 + 187.5 + 312.5, shown, 1280, 800)).toEqual({ x: 640, y: 400 });
	expect(mapPoint(5000, -10, shown, 1280, 800)).toEqual({ x: 1279, y: 0 });
	expect(fitRect(0, 10, 10, 10).width).toBe(0);
	// HiDPI: a 2× screen sends CSS pixels and the factor; the picture maps 1:1 back.
	expect(sizeMessage(640.7, 400.2, 2)).toEqual({ t: 'size', width: 640, height: 400, scale: 2 });
	expect(sizeMessage(10, 10, 0)).toMatchObject({ scale: 1 });
	expect(wheelDelta(0, 3, 1)).toEqual({ dx: 0, dy: 120 });
	expect(wheelDelta(1.4, -100, 0)).toEqual({ dx: 1, dy: -100 });
});

test('view prefs per app: volume, mute and quality remembered, broken values reset', () => {
	localStorage.clear();
	saveView('immich', { volume: 0.3, muted: true, quality: 'low', fps: 30 });
	expect(loadView('immich')).toEqual({ volume: 0.3, muted: true, quality: 'low', fps: 30 });
	// Not chosen yet: the NAS's quality applies.
	expect(loadView('jellyfin')).toEqual({ volume: 0.65, muted: false });
	localStorage.setItem('titanos.viewer.x', '{"volume":7,"quality":"ultra","fps":144}');
	expect(loadView('x')).toEqual({ volume: 0.65, muted: false });
});

test('settings and status from the NAS are read tolerantly', () => {
	expect(sanitizeSettings(null)).toEqual({
		gpu: 'auto',
		quality: 'auto',
		fps: 60,
		audio: true,
		volume: '',
	});
	expect(sanitizeSettings({ gpu: '/dev/dri/renderD128', fps: 30, audio: false })).toMatchObject({
		gpu: '/dev/dri/renderD128',
		fps: 30,
		audio: false,
	});
	const empty = sanitizeStatus({});
	expect(empty.image.state).toBe('missing');
	expect(empty.memory.more).toBe(-1);
	expect(empty.idleMinutes).toBe(-1);
	expect(empty.gpu).toBe('cpu');
	const s = sanitizeStatus({
		image: { state: 'loading', version: '0.60.0', bytes: 5, size: 10, progress: 0.5 },
		gpu: 'vaapi',
		gpus: [
			{
				id: '/dev/dri/renderD128',
				kind: 'vaapi',
				vendor: '8086',
				title: 'Intel UHD 770',
				available: true,
			},
			{ id: '0000:01:00.0', kind: 'nvenc', title: 'RTX', available: false, reason: 'vm' },
			{ title: 'no id' },
		],
		sessions: [
			{
				user: 'anna',
				app: 'immich',
				state: 'open',
				windows: 1,
				gpu: 'cpu',
				memoryBytes: 331657216,
			},
		],
		memory: { availableBytes: 1, reserveBytes: 734003200, perViewBytes: 419430400, more: 3 },
		idleMinutes: 15,
	});
	expect(s.image).toMatchObject({ state: 'loading', bytes: 5, size: 10, progress: 0.5 });
	expect(s.gpus.map((g) => [g.id, g.available, g.reason])).toEqual([
		['/dev/dri/renderD128', true, ''],
		['0000:01:00.0', false, 'vm'],
	]);
	expect(s.sessions[0]).toMatchObject({ app: 'immich', state: 'open', memoryBytes: 331657216 });
	expect(s.memory).toEqual({ more: 3, perViewBytes: 419430400, availableBytes: 1 });
	// Progress from the bytes when titand does not send it.
	expect(sanitizeImage({ state: 'downloading', bytes: 1, size: 4 }).progress).toBe(0.25);
});

test('support: WebCodecs needs a secure page', () => {
	expect(missingSupport({ VideoDecoder: function () {} })).toBeNull();
	expect(missingSupport({ isSecureContext: false })).toBe('insecure');
	expect(missingSupport({ isSecureContext: true })).toBe('webcodecs');
});
