// PLAN 66: reading and writing the files a web app opens, with the rights of
// the account (the app's frame itself has no access to the NAS).
import { api, apiFetch, responseError } from '../api.ts';
import { mtimeNs, savedMtime, type DocRef } from './protocol.ts';

/** The modification time of a file now (ns as a string), or null if it is gone. */
async function documentRevision(doc: DocRef) {
	try {
		const p = await api.get<{ modified?: string; version?: string }>(
			'/files/properties?' + new URLSearchParams({ root: doc.root, path: doc.path }),
		);
		return {
			mtime: typeof p?.modified === 'string' ? mtimeNs(p.modified) : null,
			version: p?.version || '',
		};
	} catch {
		return { mtime: null, version: '' };
	}
}
export async function currentMtime(doc: DocRef) {
	return (await documentRevision(doc)).mtime;
}

/** The file's bytes and its modification time, taken before the bytes (a change in between shows as a conflict later). */
export async function readDoc(
	doc: DocRef,
): Promise<{ bytes: ArrayBuffer; mtime: string | null; version: string }> {
	const revision = await documentRevision(doc);
	const res = await apiFetch(
		'/files/download?' + new URLSearchParams({ root: doc.root, path: doc.path }),
	);
	if (!res.ok) throw await responseError(res);
	return { bytes: await res.arrayBuffer(), ...revision };
}

export type WriteResult =
	{ ok: true; mtime: string | null; version: string } | { ok: false; conflict: true };

/**
 * PUT /files/content: replaces the file atomically when it is unchanged since
 * `expect` (its modification time in ns), or creates it with `expect` "none".
 * 409 = changed meanwhile, or already there; other errors throw.
 */
export async function writeDoc(
	doc: DocRef,
	bytes: ArrayBuffer,
	expect: string | 'none',
): Promise<WriteResult> {
	const res = await apiFetch(
		'/files/content?' + new URLSearchParams({ root: doc.root, path: doc.path, expect }),
		{ method: 'PUT', body: bytes, headers: { 'Content-Type': 'application/octet-stream' } },
	);
	if (res.status === 409) return { ok: false, conflict: true };
	if (!res.ok) throw await responseError(res);
	let mtime: string | null = null,
		version = '';
	try {
		const data = await res.json();
		mtime = savedMtime(data);
		version = typeof data?.version === 'string' ? data.version : '';
	} catch {
		// No body: asked below.
	}
	if (!mtime) {
		const revision = await documentRevision(doc);
		return { ok: true, ...revision };
	}
	return { ok: true, mtime, version };
}

/** Whether a name is free in a folder (for a copy). */
export async function namesIn(root: string, dir: string): Promise<Set<string>> {
	try {
		const list = await api.get<{ name: string }[]>(
			'/files/list?' + new URLSearchParams({ root, path: dir }),
		);
		return new Set(list.map((e) => e.name));
	} catch {
		return new Set();
	}
}
