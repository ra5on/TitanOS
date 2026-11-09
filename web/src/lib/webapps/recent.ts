// PLAN 75-18/75-20: the files of the NAS a web app opened or saved lately,
// per account and app, in this browser (opening a file must not write the
// stick). The open dialog of the app lists them first.
import type { DocRef } from './protocol.ts';

const MAX = 8;
const key = (user: string, app: string) => `titanos.webapps.recent.${user}.${app}`;

export function recentDocs(user: string, app: string): DocRef[] {
	try {
		const raw = JSON.parse(localStorage.getItem(key(user, app)) ?? '[]') as unknown;
		if (!Array.isArray(raw)) return [];
		return raw
			.filter(
				(d): d is DocRef =>
					!!d &&
					typeof d === 'object' &&
					typeof (d as DocRef).root === 'string' &&
					typeof (d as DocRef).path === 'string',
			)
			.slice(0, MAX)
			.map((d) => ({ root: d.root, path: d.path }));
	} catch {
		return [];
	}
}

export function rememberDoc(user: string, app: string, doc: DocRef) {
	const same = (d: DocRef) => d.root === doc.root && d.path === doc.path;
	const list = [
		{ root: doc.root, path: doc.path },
		...recentDocs(user, app).filter((d) => !same(d)),
	];
	try {
		localStorage.setItem(key(user, app), JSON.stringify(list.slice(0, MAX)));
	} catch {
		/* private window: not remembered */
	}
}

export function forgetDoc(user: string, app: string, doc: DocRef) {
	const list = recentDocs(user, app).filter((d) => d.root !== doc.root || d.path !== doc.path);
	try {
		localStorage.setItem(key(user, app), JSON.stringify(list));
	} catch {
		/* not remembered */
	}
}
