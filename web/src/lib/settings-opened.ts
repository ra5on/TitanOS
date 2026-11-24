import { settingsAreas } from './settings-search.ts';

const maxOpened = 3;

function key(user: string) {
	return 'titanos.settings.opened.' + user;
}

function read(user: string): string[] {
	if (!user) return [];
	try {
		const value: unknown = JSON.parse(localStorage.getItem(key(user)) ?? '[]');
		return Array.isArray(value) ? value.filter((item) => typeof item === 'string') : [];
	} catch {
		return [];
	}
}

export function recentlyOpened(user: string, admin: boolean) {
	return read(user)
		.flatMap((id) => settingsAreas.filter((area) => area.id === id && (admin || !area.admin)))
		.slice(0, maxOpened);
}

export function rememberSetting(user: string, admin: boolean, path: string) {
	if (!user) return;
	const area = settingsAreas.find(
		(area) => path === '/settings/' + area.id && (admin || !area.admin),
	);
	if (!area) return;
	try {
		const previous = read(user);
		if (previous[0] === area.id) return;
		const updated = [area.id, ...previous.filter((id) => id !== area.id)].slice(0, maxOpened);
		localStorage.setItem(key(user), JSON.stringify(updated));
	} catch {
		// Storage may be unavailable in the current browser.
	}
}
