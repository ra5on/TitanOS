// PLAN 79-7: the desktop's camera widget needs the names of the cameras the
// account may watch. Read once when the desktop starts and again when a
// camera widget is shown or the gallery opens – never in a loop.
import { api } from '../api.ts';
import type { Camera, CameraList } from './types.ts';

export const widgetCameras = $state<{ list: Camera[]; loaded: boolean }>({
	list: [],
	loaded: false,
});

let loading: Promise<void> | null = null;
let at = 0;

/** Reads the cameras (at most once a minute unless `force`). */
export function loadWidgetCameras(force = false): Promise<void> {
	if (loading) return loading;
	if (!force && widgetCameras.loaded && Date.now() - at < 60000) return Promise.resolve();
	loading = api
		.get<CameraList>('/nvr/cameras')
		.then(
			(res) => {
				widgetCameras.list = (res.cameras ?? [])
					.filter((c) => !c.deleted && c.enabled && (!c.rights || c.rights.live))
					.sort((a, b) => a.order - b.order || a.id.localeCompare(b.id));
			},
			() => {
				// A NAS without the NVR, or an NVR that is off: no cameras to offer.
				widgetCameras.list = [];
			},
		)
		.finally(() => {
			widgetCameras.loaded = true;
			at = Date.now();
			loading = null;
		});
	return loading;
}
