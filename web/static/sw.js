// Service worker of the TitanOS web app: only shows notifications that the
// NAS sends over Web Push (docs/WEB-PUSH.md). It caches nothing; the web UI
// always comes from the NAS.
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
			// The agent's message goes to an open agent window; others to any
			// TitanOS window. The page moves itself (no reload, so nothing
			// unsaved in an editor or a console is lost), PLAN 59.
			const target = agent(url.pathname)
				? same.find((c) => agent(new URL(c.url).pathname))
				: same[0];
			if (target) {
				await target.focus();
				target.postMessage({ type: 'open', url: url.pathname + url.search });
				return;
			}
			await self.clients.openWindow(url.href);
		})(),
	);
});
			for (const client of windows) {
				if (new URL(client.url).origin === self.location.origin) {
					await client.focus();
					if ('navigate' in client) await client.navigate(url);
					return;
				}
			}
			await self.clients.openWindow(url);
		})(),
	);
});
