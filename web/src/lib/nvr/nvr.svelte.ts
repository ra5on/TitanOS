// PLAN 79-7: what every page of the NVR needs – the state of the NVR and the
// cameras this account may see. One poller while a page of the NVR is shown.
import { api, ApiError } from '../api.ts';
import { apiErrorText } from '../i18n/index.svelte.ts';
import { poll, type Poller } from '../poll.ts';
import { session } from '../session.svelte.ts';
import type { Camera, CameraList, NvrStatus } from './types.ts';

export const nvr = $state<{
	status: NvrStatus | null;
	/** The cameras as they are (without removed ones). */
	cameras: Camera[];
	/** Removed cameras whose recordings are still kept (administrators only). */
	removed: Camera[];
	/** The objects a camera can look for (administrators). */
	objects: string[];
	loaded: boolean;
	error: string;
}>({ status: null, cameras: [], removed: [], objects: [], loaded: false, error: '' });

export function nvrAdmin(): boolean {
	return ['owner', 'admin'].includes(session.data?.user?.role ?? '');
}

/** Whether the account may watch this camera live / see its recordings. */
export function mayLive(camera: Camera): boolean {
	return nvrAdmin() || camera.rights?.live === true;
}
export function mayRecordings(camera: Camera): boolean {
	return nvrAdmin() || camera.rights?.recordings === true;
}

const byOrder = (a: Camera, b: Camera) => a.order - b.order || a.id.localeCompare(b.id);

async function read(signal?: AbortSignal) {
	try {
		const [status, list] = await Promise.all([
			api.get<NvrStatus>('/nvr/status', signal),
			api.get<CameraList>('/nvr/cameras', signal),
		]);
		nvr.status = status;
		const all = (list.cameras ?? []).map(tidyCamera).sort(byOrder);
		nvr.cameras = all.filter((c) => !c.deleted);
		nvr.removed = all.filter((c) => c.deleted);
		nvr.objects = list.objects ?? [];
		nvr.error = '';
	} catch (err) {
		if (signal?.aborted) return;
		// A NAS without the NVR answers nvr_unavailable: say so, do not retry loudly.
		nvr.error = apiErrorText(err);
		if (err instanceof ApiError && err.code === 'nvr_unavailable')
			nvr.status = {
				enabled: false,
				state: 'unavailable',
				cameras: { total: 0, enabled: 0, online: 0 },
				privacyNoticeDismissed: true,
			};
	} finally {
		nvr.loaded = true;
	}
}

let poller: Poller | null = null;

/** Reads now, e.g. right after a change. */
export function refreshNvr(): Promise<void> {
	return poller ? poller.now() : read();
}

/** Starts following the NVR; the result stops it. Faster while it starts up. */
export function watchNvr(): () => void {
	const busy = () =>
		['pulling', 'starting', 'docker_starting'].includes(nvr.status?.state ?? '') ? 2000 : 6000;
	const current = poll(read, { interval: busy });
	poller = current;
	return () => {
		current();
		if (poller === current) poller = null;
	};
}

/** The name of a camera, also of a removed one (events and exports keep their camera). */
export function cameraName(id: string): string {
	return (nvr.cameras.find((c) => c.id === id) ?? nvr.removed.find((c) => c.id === id))?.name ?? id;
}

/** A time of the API as Unix seconds: numbers are seconds, text is ISO. */
export function unixOf(value: number | string | null | undefined): number | null {
	if (typeof value === 'number' && Number.isFinite(value)) return value;
	if (typeof value === 'string' && value) {
		const ms = Date.parse(value);
		return Number.isNaN(ms) ? null : ms / 1000;
	}
	return null;
}

/** The address of a camera's still picture; `tick` makes the browser ask again. */
export function snapshotPath(id: string, height: number, tick: number): string {
	return `/nvr/cameras/${encodeURIComponent(id)}/snapshot.jpg?height=${height}&t=${tick}`;
}

/**
 * Lists a NAS of 0.79 sends as null for a camera stored by 0.78 (a window
 * without days, owner 13.11.2026: the camera form failed with 500) become
 * empty lists, so that no view has to guard each of them.
 */
export function tidyCamera(camera: Camera): Camera {
	const n = camera.notify;
	return {
		...camera,
		...(camera.detect
			? { detect: { ...camera.detect, objects: camera.detect.objects ?? [] } }
			: {}),
		...(n
			? {
					notify: {
						...n,
						kinds: n.kinds ?? [],
						...(n.window ? { window: { ...n.window, days: n.window.days ?? [] } } : {}),
					},
				}
			: {}),
	};
}
