// The upload queue. It lives outside any page, so uploads keep running
// while the user moves around the UI.

import { api, ApiError, type UploadStatus } from './api.ts';

/** Size of one request. A lost connection costs at most this much again. */
const CHUNK_BYTES = 8 * 1024 * 1024;

export type UploadState = 'queued' | 'uploading' | 'paused' | 'retrying' | 'conflict' | 'done' | 'failed';

export type Upload = {
	key: number;
	file: File;
	root: string;
	rootName: string;
	/** Folder inside the root, "" for its top. */
	dir: string;
	name: string;
	sent: number;
	state: UploadState;
	/** Bytes per second, smoothed. */
	speed: number;
	error: string;
	/** What to do if the name exists; set once the user decided. */
	conflict: '' | 'replace' | 'rename';
	id: string;
	/** A request for this upload is on its way. */
	busy: boolean;
};

export const uploads = $state<{ items: Upload[]; finished: number }>({ items: [], finished: 0 });

let nextKey = 1;
let running = false;

/** Adds files to the queue. `dir` is the folder they go to inside the root. */
export function enqueue(files: { file: File; dir: string }[], root: { id: string; name: string }) {
	for (const { file, dir } of files) {
		uploads.items.push({
			key: nextKey++, file, root: root.id, rootName: root.name, dir, name: file.name,
			sent: 0, state: 'queued', speed: 0, error: '', conflict: '', id: '', busy: false
		});
	}
	work();
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

async function work() {
	if (running) return;
	running = true;
	for (;;) {
		const item = uploads.items.find((upload) => upload.state === 'queued' && !upload.busy);
		if (!item) break;
		item.busy = true;
		await transfer(item);
		item.busy = false;
		if (item.state === 'done') uploads.finished++;
	}
	running = false;
}

async function transfer(item: Upload) {
	item.state = 'uploading';
	try {
		if (!item.id) {
			const status = await api.post<UploadStatus>('/files/uploads', {
				root: item.root, dir: item.dir, name: item.name, size: item.file.size, conflict: item.conflict
			});
			item.id = status.id;
			item.name = status.name;
		}
		let failures = 0;
		while (item.sent < item.file.size) {
			// Reading state through a function keeps TypeScript from assuming it cannot change meanwhile.
			if (stateOf(item) !== 'uploading' && stateOf(item) !== 'retrying') return;
			const started = performance.now();
			try {
				const res = await fetch(`/api/v1/files/uploads/${item.id}?offset=${item.sent}`, {
					method: 'PATCH', credentials: 'same-origin', body: item.file.slice(item.sent, item.sent + CHUNK_BYTES)
				});
				if (res.status === 409) {
					// The NAS has more or less than we think: ask and continue there.
					item.sent = (await api.get<UploadStatus>(`/files/uploads/${item.id}`)).offset;
					continue;
				}
				if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => null))?.error?.code ?? 'internal', '');
				const status: UploadStatus = await res.json();
				const seconds = (performance.now() - started) / 1000;
				const rate = seconds > 0 ? (status.offset - item.sent) / seconds : 0;
				item.speed = item.speed ? item.speed * 0.7 + rate * 0.3 : rate;
				item.sent = status.offset;
				// Back to normal after a lost connection – but a pause or cancel
				// that came in while this request was on its way stays.
				if (stateOf(item) === 'retrying') item.state = 'uploading';
				failures = 0;
			} catch (err) {
				if (err instanceof ApiError) throw err;
				// The connection dropped. Wait, find out what arrived, go on.
				if (++failures > 30) throw err;
				if (stateOf(item) === 'uploading') item.state = 'retrying';
				await sleep(Math.min(2000 * failures, 15000));
				try {
					item.sent = (await api.get<UploadStatus>(`/files/uploads/${item.id}`)).offset;
				} catch (statusErr) {
					if (statusErr instanceof ApiError && statusErr.status === 404) throw statusErr;
				}
			}
		}
		const done = await api.post<UploadStatus>(`/files/uploads/${item.id}/finish`);
		item.name = done.name;
		item.state = 'done';
	} catch (err) {
		if (err instanceof ApiError && err.code === 'file_exists' && !item.id) {
			item.state = 'conflict';
			return;
		}
		item.error = err instanceof ApiError ? err.code : 'unreachable';
		item.state = 'failed';
	}
}

function stateOf(item: Upload): UploadState {
	return item.state;
}

export function pause(item: Upload) {
	if (item.state === 'uploading' || item.state === 'retrying' || item.state === 'queued') item.state = 'paused';
}

export function resume(item: Upload) {
	if (item.state !== 'paused') return;
	// If the last request before the pause is still on its way, the loop
	// that sent it simply carries on; otherwise the upload queues up again.
	item.state = item.busy ? 'uploading' : 'queued';
	work();
}

/** Answers "this name exists": replace it, keep both, or skip the file. */
export function resolve(item: Upload, decision: 'replace' | 'rename' | 'skip') {
	if (item.state !== 'conflict') return;
	if (decision === 'skip') return void remove(item);
	item.conflict = decision;
	item.state = 'queued';
	work();
}

/** Stops an upload and removes what has arrived of it. */
export async function cancel(item: Upload) {
	const id = item.id;
	item.state = 'paused';
	remove(item);
	// A request that is still on its way holds the upload for a moment.
	for (let attempt = 0; id && attempt < 5; attempt++) {
		try {
			await api.delete(`/files/uploads/${id}`);
			return;
		} catch {
			await sleep(600);
		}
	}
}

function remove(item: Upload) {
	const index = uploads.items.indexOf(item);
	if (index >= 0) uploads.items.splice(index, 1);
}

/** Drops finished and failed uploads from the list. */
export function clearFinished() {
	uploads.items = uploads.items.filter((item) => item.state !== 'done' && item.state !== 'failed');
}
