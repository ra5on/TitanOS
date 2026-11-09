import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import vm from 'node:vm';

// PLAN 59: a tapped notification moves an open agent window without a
// reload and never takes over another TitanOS window for the agent.
function load(windows) {
	const handlers = {};
	const opened = [];
	const self = {
		location: { origin: 'https://nas.local' },
		addEventListener: (type, fn) => (handlers[type] = fn),
		clients: {
			matchAll: async () => windows,
			openWindow: async (u) => opened.push(u),
			claim: async () => {},
		},
		registration: { showNotification: async () => {} },
		skipWaiting: () => {},
	};
	vm.runInNewContext(readFileSync(new URL('../static/sw.js', import.meta.url), 'utf8'), {
		self,
		URL,
		console,
	});
	return { handlers, opened };
}
function win(url) {
	const w = { url, focused: false, sent: [] };
	w.focus = async () => (w.focused = true);
	w.postMessage = (m) => w.sent.push(m);
	return w;
}
async function click(sw, url) {
	let done;
	sw.handlers.notificationclick({
		notification: { close() {}, data: { url } },
		waitUntil: (p) => (done = p),
	});
	await done;
}

test('agent message goes to the agent window', async () => {
	const files = win('https://nas.local/files');
	const agent = win('https://nas.local/agent?app=1');
	const sw = load([files, agent]);
	await click(sw, '/agent?chat=abc');
	assert.deepEqual(JSON.parse(JSON.stringify(agent.sent)), [
		{ type: 'open', url: '/agent?chat=abc' },
	]);
	assert.equal(files.sent.length, 0);
});

// PLAN 75-16: without an agent window the tap still lands in the chat: an
// open TitanOS window (the desktop first) opens the agent; an installed app
// brought forward by openWindow would keep its page (the home screen).
test('without an agent window the desktop opens the chat', async () => {
	const files = win('https://nas.local/files');
	const desk = win('https://nas.local/');
	const sw = load([files, desk]);
	await click(sw, '/agent?chat=abc');
	assert.deepEqual(JSON.parse(JSON.stringify(desk.sent)), [
		{ type: 'open', url: '/agent?chat=abc' },
	]);
	assert.equal(desk.focused, true);
	assert.equal(files.sent.length, 0);
	assert.deepEqual([...sw.opened], []);
});

test('any TitanOS window opens the chat when no desktop is open', async () => {
	const files = win('https://nas.local/files');
	const sw = load([files]);
	await click(sw, '/agent?chat=abc');
	assert.deepEqual(JSON.parse(JSON.stringify(files.sent)), [
		{ type: 'open', url: '/agent?chat=abc' },
	]);
});

test('without any TitanOS window a new one opens at the chat', async () => {
	const sw = load([]);
	await click(sw, '/agent?chat=abc');
	assert.deepEqual([...sw.opened], ['https://nas.local/agent?chat=abc']);
});

test('a NAS message uses any TitanOS window', async () => {
	const files = win('https://nas.local/files');
	const sw = load([files]);
	await click(sw, '/');
	assert.deepEqual(JSON.parse(JSON.stringify(files.sent)), [{ type: 'open', url: '/' }]);
});
