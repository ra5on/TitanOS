import type { User } from './api.ts';
import { SYSTEM_APPS, appAllowed } from './desktop/apps.ts';

export function startApps(user: User, services: Record<string, boolean>) {
	return SYSTEM_APPS.filter(
		(app) =>
			['files', 'backup', 'calendar', 'contacts', 'mail', 'agent'].includes(app.id) &&
			appAllowed(app, user.role, services),
	);
}

export function savedStart(user: User, services: Record<string, boolean>) {
	try {
		const path = localStorage.getItem('titanos.start.' + user.name);
		return startApps(user, services).find((app) => app.href === path)?.href ?? '/';
	} catch {
		return '/';
	}
}
