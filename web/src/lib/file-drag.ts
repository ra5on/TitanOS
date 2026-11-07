// The payload stays in this page's memory: external text/URLs cannot authorize a transfer.
export const fileDragType = 'application/x-titanos-files';
export type FileDrag = { root: string; paths: string[] };
let active: FileDrag | null = null;
/** PLAN 66: a drag that started in the file manager in a window (a frame of our own origin). */
let fromWindow: FileDrag | null = null;

/** Inside a window of the desktop the desktop learns of the drag (it may end on a web app's window). */
function tellDesktop(value: FileDrag | null) {
	try {
		if (window.parent !== window && window.parent.location.origin === location.origin)
			window.parent.postMessage({ type: 'titanos-file-drag', drag: value }, location.origin);
	} catch {
		// Not in a window of the desktop.
	}
}
export function beginFileDrag(event: DragEvent, value: FileDrag) {
	if (!event.dataTransfer) return;
	active = { root: value.root, paths: [...value.paths] };
	event.dataTransfer.setData(fileDragType, '1');
	event.dataTransfer.effectAllowed = 'copyMove';
	tellDesktop(active);
}
export function endFileDrag() {
	active = null;
	tellDesktop(null);
}
/** The desktop: the drag a window of our own origin reported (or its end). */
export function setWindowDrag(value: FileDrag | null) {
	fromWindow =
		value && typeof value.root === 'string' && Array.isArray(value.paths)
			? { root: value.root, paths: value.paths.filter((p) => typeof p === 'string') }
			: null;
}
export function hasFileDrag(event: DragEvent) {
	return !!(active ?? fromWindow) && !!event.dataTransfer?.types.includes(fileDragType);
}
export function takeFileDrag(event: DragEvent) {
	if (!hasFileDrag(event)) return null;
	const result = active ?? fromWindow;
	active = null;
	fromWindow = null;
	return result;
}
