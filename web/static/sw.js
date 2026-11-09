// Service worker of the TitanOS web app: shows notifications that the NAS
// sends over Web Push (docs/WEB-PUSH.md) and takes files shared to the
// installed app (PLAN 70, docs/PWA.md). It caches nothing of the web UI or
// the API; the web UI always comes from the NAS.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

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
					list.push({ key, name: file.name || `datei-${i + 1}`, size: file.size, type: file.type });
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
				return Response.redirect('/share?missing=1', 303);
			}
		})(),
	);
});
