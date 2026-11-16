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

// Durchsicht 102: a share that cannot be stored completely (quota) leaves
// nothing half-stored behind, and a file without a name gets no German
// name from the worker (the share page names it in the user's language).
function shareWorker(failAt) {
	const handlers = {};
	const store = new Map();
	const deleted = [];
	let puts = 0;
	const caches = {
		delete: async (name) => {
			deleted.push(name);
			store.clear();
			return true;
		},
		open: async () => ({
			put: async (key, res) => {
				if (++puts === failAt) throw new Error('QuotaExceededError');
				store.set(key, await res.text());
			},
		}),
	};
	const self = {
		location: { origin: 'https://nas.local' },
		addEventListener: (t, fn) => (handlers[t] = fn),
	};
	vm.runInNewContext(readFileSync(new URL('../static/sw.js', import.meta.url), 'utf8'), {
		self,
		URL,
		console,
		caches,
		// Node resolves no relative redirect (a worker resolves it against its own address).
		Response: class extends Response {
			static redirect(url, status) {
				return new Response(null, { status, headers: { Location: url } });
			}
		},
		File,
	});
	async function share(files) {
		const form = new FormData();
		for (const f of files) form.append('files', f);
		let answer;
		handlers.fetch({
			request: {
				method: 'POST',
				url: 'https://nas.local/share-target',
				formData: async () => form,
			},
			respondWith: (p) => (answer = p),
		});
		return answer;
	}
	return { share, store, deleted };
}

test('a share that cannot be stored completely leaves nothing behind', async () => {
	const sw = shareWorker(2);
	const res = await sw.share([new File(['a'], 'a.txt'), new File(['b'], 'b.txt')]);
	assert.equal(res.headers.get('Location'), '/share?missing=1');
	assert.equal(sw.store.size, 0);
	assert.equal(sw.deleted.length, 2);
});

test('a shared file without a name is named by the page, not the worker', async () => {
	const sw = shareWorker(0);
	await sw.share([new File(['a'], '')]);
	const list = JSON.parse(sw.store.get('/share-target/list'));
	assert.equal(list.files[0].name, '');
});
