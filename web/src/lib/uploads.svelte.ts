// The upload queue. It lives outside any page, so uploads keep running
// while the user moves around the UI.

import { apiPath } from '#lib/cluster-node.ts';
import { api, ApiError, type UploadStatus } from './api.ts';

/** Size of one request. A lost connection costs at most this much again. */
const CHUNK_BYTES = 8 * 1024 * 1024;

export type UploadState =
	'queued' | 'uploading' | 'paused' | 'retrying' | 'pressure' | 'conflict' | 'done' | 'failed';

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

export const uploads = $state<{ items: Upload[]; finished: number; paused: boolean }>({
	items: [],
	finished: 0,
	paused: false,
});

let nextKey = 1;
let running = false;

/** Adds files to the queue. `dir` is the folder they go to inside the root. */
export function enqueue(
	files: { file: File; dir: string; conflict?: '' | 'rename' | 'replace' }[],
	root: { id: string; name: string },
) {
	for (const { file, dir, conflict = '' } of files) {
		uploads.items.push({
			key: nextKey++,
			file,
			root: root.id,
			rootName: root.name,
			dir,
			name: file.name,
			sent: 0,
			state: uploads.paused ? 'paused' : 'queued',
			speed: 0,
			error: '',
			conflict,
			id: '',
			busy: false,
		});
	}
	work();
}

function receivedOffset(item: Upload, value: number) {
	if (!Number.isSafeInteger(value) || value < 0 || value > item.file.size)
		throw new ApiError(500, 'internal', 'Invalid upload offset');
	return value;
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
				root: item.root,
				dir: item.dir,
				name: item.name,
				size: item.file.size,
				conflict: item.conflict,
			});
			item.id = status.id;
			item.name = status.name;
		} else {
			// A restarted daemon rediscovers persistent partial uploads here.
			await api.get<UploadStatus[]>(
				`/files/uploads?root=${encodeURIComponent(item.root)}&path=${encodeURIComponent(item.dir)}`,
			);
			item.sent = receivedOffset(
				item,
				(await api.get<UploadStatus>(`/files/uploads/${item.id}`)).offset,
			);
		}
		let failures = 0;
		while (item.sent < item.file.size) {
			// Reading state through a function keeps TypeScript from assuming it cannot change meanwhile.
			if (
				stateOf(item) !== 'uploading' &&
				stateOf(item) !== 'retrying' &&
				stateOf(item) !== 'pressure'
			)
				return;
			const started = performance.now();
			try {
				const res = await fetch(apiPath(`/files/uploads/${item.id}?offset=${item.sent}`), {
					method: 'PATCH',
					credentials: 'same-origin',
					body: item.file.slice(item.sent, item.sent + CHUNK_BYTES),
				});
				const problem = !res.ok
					? ((await res.json().catch(() => null))?.error?.code ?? 'internal')
					: '';
				if (problem === 'upload_memory_pressure') {
					if (stateOf(item) === 'uploading' || stateOf(item) === 'retrying')
						item.state = 'pressure';
					item.speed = 0;
					await sleep(3000);
					item.sent = receivedOffset(
						item,
						(await api.get<UploadStatus>(`/files/uploads/${item.id}`)).offset,
					);
					continue;
				}
				if (problem === 'upload_mismatch') {
					// The NAS has more or less than we think: ask and continue there.
					const current = await api.get<UploadStatus>(`/files/uploads/${item.id}`);
					item.sent = receivedOffset(item, current.offset);
					if (current.busy) await sleep(500);
					continue;
				}
				if (!res.ok) throw new ApiError(res.status, problem, '');
				const status: UploadStatus = await res.json();
				receivedOffset(item, status.offset);
				const seconds = (performance.now() - started) / 1000;
				const rate = seconds > 0 ? (status.offset - item.sent) / seconds : 0;
				item.speed = item.speed ? item.speed * 0.7 + rate * 0.3 : rate;
				item.sent = status.offset;
				// Back to normal after a lost connection – but a pause or cancel
				// that came in while this request was on its way stays.
				if (stateOf(item) === 'retrying' || stateOf(item) === 'pressure') item.state = 'uploading';
				failures = 0;
			} catch (err) {
				if (err instanceof ApiError) throw err;
				// The connection dropped. Wait, find out what arrived, go on.
				if (++failures > 30) throw err;
				if (stateOf(item) === 'uploading') item.state = 'retrying';
				await sleep(Math.min(2000 * failures, 15000));
				try {
					item.sent = receivedOffset(
						item,
						(await api.get<UploadStatus>(`/files/uploads/${item.id}`)).offset,
					);
				} catch (statusErr) {
					if (statusErr instanceof ApiError && statusErr.status === 404) throw statusErr;
				}
			}
		}
		if (stateOf(item) === 'paused') return;
		const done = await api.post<UploadStatus>(`/files/uploads/${item.id}/finish`);
		item.name = done.name;
		item.state = 'done';
	} catch (err) {
		if (stateOf(item) === 'paused') {
			item.error = err instanceof ApiError ? err.code : 'unreachable';
			return;
		}
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
	if (
		item.state === 'uploading' ||
		item.state === 'retrying' ||
		item.state === 'pressure' ||
		item.state === 'queued'
	)
		item.state = 'paused';
}

export function resume(item: Upload) {
	if (item.state !== 'paused' && item.state !== 'failed') return;
	if (item.error === 'upload_not_found') {
		item.id = '';
		item.sent = 0;
	}
	item.error = '';
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

/** Verify the complete received prefix before resuming a reselected file. */
export async function recover(
	file: File,
	root: { id: string; name: string },
	dir: string,
	status: UploadStatus,
) {
	if (file.size !== status.size) throw new ApiError(409, 'upload_mismatch', '');
	for (let offset = 0; offset < status.offset; offset += CHUNK_BYTES) {
		const res = await fetch(apiPath(`/files/uploads/${status.id}/verify?offset=${offset}`), {
			method: 'POST',
			credentials: 'same-origin',
			body: file.slice(offset, Math.min(offset + CHUNK_BYTES, status.offset)),
		});
		if (!res.ok)
			throw new ApiError(
				res.status,
				(await res.json().catch(() => null))?.error?.code ?? 'internal',
				'',
			);
	}
	if (uploads.items.some((item) => item.id === status.id)) return;
	uploads.items.push({
		key: nextKey++,
		file,
		root: root.id,
		rootName: root.name,
		dir,
		name: status.name,
		sent: status.offset,
		state: 'queued',
		speed: 0,
		error: '',
		conflict: '',
		id: status.id,
		busy: false,
	});
	work();
}

/** Stops an upload and removes what has arrived of it. */
export async function cancel(item: Upload) {
	item.state = 'paused';
	// Let an in-flight request release its file before asking to remove it.
	for (let attempt = 0; attempt < 30 && item.busy; attempt++) await sleep(500);
	try {
		if (item.id) await api.delete(`/files/uploads/${item.id}`);
		remove(item);
		uploads.finished++;
	} catch (err) {
		item.error = err instanceof ApiError ? err.code : 'unreachable';
		item.state = 'failed';
	}
}

function remove(item: Upload) {
	const index = uploads.items.indexOf(item);
	if (index >= 0) uploads.items.splice(index, 1);
}

/** Drops completed uploads; failed items stay available for retry. */
export function clearFinished() {
	uploads.items = uploads.items.filter((item) => item.state !== 'done');
}

/** Queue controls retain each File and confirmed conflict choice in this tab. */
export function pauseAll() {
	uploads.paused = true;
	for (const item of uploads.items) pause(item);
}
export function resumeAll() {
	uploads.paused = false;
	for (const item of uploads.items) if (item.state === 'paused') resume(item);
}
export function retryFailed() {
	for (const item of uploads.items) if (item.state === 'failed') resume(item);
}
