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

test('without an agent window a new one opens', async () => {
	const files = win('https://nas.local/files');
	const sw = load([files]);
	await click(sw, '/agent?chat=abc');
	assert.deepEqual([...sw.opened], ['https://nas.local/agent?chat=abc']);
	assert.equal(files.focused, false);
});

test('a NAS message uses any TitanOS window', async () => {
	const files = win('https://nas.local/files');
	const sw = load([files]);
	await click(sw, '/');
	assert.deepEqual(JSON.parse(JSON.stringify(files.sent)), [{ type: 'open', url: '/' }]);
});
