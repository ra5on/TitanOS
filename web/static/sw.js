// Service worker of the TitanOS web app: shows notifications that the NAS
// sends over Web Push (docs/WEB-PUSH.md), takes files shared to the
// installed app (PLAN 70, docs/PWA.md) and, since 1.07, keeps the page
// itself so that the installed app opens without the NAS (docs/PWA.md
// "Ohne Verbindung"). It never keeps anything of the API.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) =>
	event.waitUntil(
		(async () => {
			// Caches of other versions of this worker go; the shared files
			// (titanos-share) are never touched here.
			for (const name of await caches.keys())
				if (
					(name.startsWith('titanos-shell-') && name !== SHELL_CACHE) ||
					(name.startsWith('titanos-assets-') && name !== ASSET_CACHE)
				)
					await caches.delete(name);
			await self.clients.claim();
		})(),
	),
);

// 1.07: the page without the NAS. A page of the web UI (a route of the app,
// not a file, not the API, not a web app or the manual) comes from the NAS
// as before; its HTML is kept – one for TitanOS, one for the agent app,
// whose HTML differs a little. When the NAS does not answer within a few
// seconds or a gateway returns 502, 503 or 504, the kept HTML is the answer,
// and the page shows the last state
// it saw (lib/offline.svelte.ts). The hashed files of the UI
// (/_app/immutable/) are kept as the page loads them: a part of TitanOS
// that was never opened with the NAS is not there without it. A new
// version of the UI (another entry file in the HTML) empties the kept files.
const SHELL_CACHE = 'titanos-shell-v1';
const ASSET_CACHE = 'titanos-assets-v1';
const SHELL_WAIT_MS = 6000;
const NOT_APP = /^\/(api|_app|webapps|manual|icons|backgrounds|share-target)(\/|$)/;
const ENTRY = /\/_app\/immutable\/entry\/start\.[A-Za-z0-9_-]+\.js/;

function appRoute(url) {
	if (NOT_APP.test(url.pathname)) return false;
	const last = url.pathname.split('/').pop() || '';
	return !last.includes('.');
}
const shellKey = (url) =>
	url.pathname === '/agent' || url.pathname.startsWith('/agent/')
		? '/__shell/agent'
		: '/__shell/main';

async function keepShell(url, res) {
	try {
		if (!res.ok || res.redirected || res.type !== 'basic') return;
		if (!(res.headers.get('Content-Type') || '').includes('text/html')) return;
		const html = await res.clone().text();
		const entry = html.match(ENTRY)?.[0];
		if (!entry) return;
		const cache = await caches.open(SHELL_CACHE);
		const before = await cache.match('/__shell/version');
		const version = before ? await before.text() : '';
		if (version !== entry) {
			// Another version of the UI: its files replace the kept ones.
			await caches.delete(ASSET_CACHE);
			for (const key of ['/__shell/main', '/__shell/agent']) await cache.delete(key);
			await cache.put('/__shell/version', new Response(entry));
		}
		const headers = new Headers(res.headers);
		headers.set('X-TitanOS-Kept', '1');
		await cache.put(shellKey(url), new Response(html, { status: 200, statusText: 'OK', headers }));
	} catch {
		// Not kept this time (quota, private mode); it is next time.
	}
}

async function navigate(event, url) {
	const network = fetch(event.request);
	// Kept whenever the NAS answered, even after the kept page was shown.
	event.waitUntil(network.then((res) => keepShell(url, res)).catch(() => {}));
	const kept = () =>
		caches
			.open(SHELL_CACHE)
			.then((cache) => cache.match(shellKey(url)))
			.catch(() => undefined);
	let timer;
	try {
		const res = await Promise.race([
			network,
			new Promise((_, fail) => {
				timer = setTimeout(() => fail(new Error('slow')), SHELL_WAIT_MS);
			}),
		]);
		if ([502, 503, 504].includes(res.status)) return (await kept()) ?? res;
		return res;
	} catch {
		const page = await kept();
		if (page) return page;
		return network; // nothing kept: the browser's own error
	} finally {
		clearTimeout(timer);
	}
}

async function asset(event) {
	const cache = await caches.open(ASSET_CACHE);
	const hit = await cache.match(event.request);
	if (hit) return hit;
	const res = await fetch(event.request);
	if (res.ok && res.type === 'basic')
		event.waitUntil(cache.put(event.request, res.clone()).catch(() => {}));
	return res;
}

self.addEventListener('fetch', (event) => {
	const req = event.request;
	if (req.method !== 'GET') return;
	const url = new URL(req.url);
	if (url.origin !== self.location.origin) return;
	if (req.mode === 'navigate' && appRoute(url)) {
		event.respondWith(navigate(event, url));
		return;
	}
	if (url.pathname.startsWith('/_app/immutable/')) event.respondWith(asset(event));
});

self.addEventListener('push', (event) => {
	let message = {};
	try {
		message = event.data ? event.data.json() : {};
	} catch {
		message = { title: 'TitanOS', body: event.data ? event.data.text() : '' };
	}
	const url = typeof message.url === 'string' && message.url.startsWith('/') ? message.url : '/';
	event.waitUntil(
		self.registration.showNotification(message.title || 'TitanOS', {
			body: message.body || '',
			tag: message.tag || undefined,
			icon: '/icons/app-192.png',
			badge: '/icons/app-192.png',
			data: { url },
		}),
	);
});

self.addEventListener('notificationclick', (event) => {
	event.notification.close();
	const url = new URL(event.notification.data?.url || '/', self.location.origin);
	const agent = (path) => path === '/agent' || path.startsWith('/agent/');
	event.waitUntil(
		(async () => {
			const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
			const same = windows.filter((c) => new URL(c.url).origin === self.location.origin);
			// The agent's message goes to an open agent window, else – like any
			// other – to any TitanOS window, which opens the agent with that
			// chat (desktop: its window; phone: the agent app). A new window
			// only when none is open: an installed app brought forward by
			// openWindow keeps its page and would show the home screen
			// (PLAN 75-16). The page moves itself (no reload, so nothing
			// unsaved in an editor or a console is lost), PLAN 59.
			const home = same.find((c) => new URL(c.url).pathname === '/');
			const target =
				(agent(url.pathname) && same.find((c) => agent(new URL(c.url).pathname))) ||
				home ||
				same[0];
			if (target) {
				await target.focus();
				target.postMessage({ type: 'open', url: url.pathname + url.search });
				return;
			}
			await self.clients.openWindow(url.href);
		})(),
	);
});

// PLAN 70: the share target. Android hands shared files over as a POST to
// /share-target; they are kept in this browser only (Cache Storage
// "titanos-share", nothing is sent anywhere) until /share uploads them to a
// folder the user chooses, or drops them.
const SHARE_CACHE = 'titanos-share';
self.addEventListener('fetch', (event) => {
	const url = new URL(event.request.url);
	if (
		event.request.method !== 'POST' ||
		url.origin !== self.location.origin ||
		url.pathname !== '/share-target'
	)
		return;
	event.respondWith(
		(async () => {
			try {
				const form = await event.request.formData();
				await caches.delete(SHARE_CACHE);
				const cache = await caches.open(SHARE_CACHE);
				const files = form.getAll('files').filter((f) => f instanceof File && f.size >= 0);
				const list = [];
				for (const [i, file] of files.entries()) {
					const key = `/share-target/file/${i}`;
					await cache.put(
						key,
						new Response(file, {
							headers: { 'Content-Type': file.type || 'application/octet-stream' },
						}),
					);
					// Without a name the share page names it in the user's language.
					list.push({ key, name: file.name || '', size: file.size, type: file.type });
				}
				const text = ['title', 'text', 'url']
					.map((k) => form.get(k))
					.filter((v) => typeof v === 'string' && v.trim())
					.join('\n');
				await cache.put(
					'/share-target/list',
					new Response(JSON.stringify({ files: list, text }), {
						headers: { 'Content-Type': 'application/json' },
					}),
				);
				return Response.redirect('/share', 303);
			} catch {
				// Nothing half-stored stays behind (e.g. the browser's quota ran out).
				await caches.delete(SHARE_CACHE).catch(() => {});
				return Response.redirect('/share?missing=1', 303);
			}
		})(),
	);
});
