// PLAN 87: "Teilen" for a file on the phone – the system's share sheet
// (Web Share API with files) hands it to another app (mail, chat …). TitanOS
// has no share links (owner's decision, docs/rules/SHARES-FILES.md); this
// only passes a copy of the file. Without the API, or for large files, the
// file is saved instead.

/** Larger files are saved, not shared (they would lie in the phone's memory first). */
export const SHARE_LIMIT = 100 * 1024 * 1024;

export type ShareResult = 'shared' | 'cancelled' | 'saved' | 'again' | 'failed';

type ShareNavigator = Navigator & {
	canShare?: (data: ShareData) => boolean;
};

/** Whether this browser can share files at all. */
export function canShareFiles(): boolean {
	const nav = navigator as ShareNavigator;
	if (typeof nav.share !== 'function' || typeof nav.canShare !== 'function') return false;
	try {
		return nav.canShare({ files: [new File([''], 'x.txt', { type: 'text/plain' })] });
	} catch {
		return false;
	}
}

/**
 * Loads the file and opens the share sheet. "again": the browser wants a
 * fresh tap (loading took too long); the caller offers a button that calls
 * `shareLoaded` with the same file.
 */
export async function shareFile(
	url: string,
	name: string,
	size: number,
	save: () => void,
): Promise<{ result: ShareResult; file?: File }> {
	if (!canShareFiles() || size > SHARE_LIMIT) {
		save();
		return { result: 'saved' };
	}
	let file: File;
	try {
		const res = await fetch(url, { credentials: 'same-origin' });
		if (!res.ok) return { result: 'failed' };
		const blob = await res.blob();
		file = new File([blob], name, { type: blob.type || 'application/octet-stream' });
	} catch {
		return { result: 'failed' };
	}
	return { result: await shareLoaded(file), file };
}

export async function shareLoaded(file: File): Promise<ShareResult> {
	const nav = navigator as ShareNavigator;
	if (!nav.canShare?.({ files: [file] })) return 'failed';
	try {
		await nav.share({ files: [file], title: file.name });
		return 'shared';
	} catch (err) {
		const name = (err as DOMException)?.name;
		if (name === 'AbortError') return 'cancelled';
		if (name === 'NotAllowedError') return 'again';
		return 'failed';
	}
}
