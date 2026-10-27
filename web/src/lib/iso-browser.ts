// Paths for the ISO browser of the VM pages. Installation media must lie on
// a volume (`/mnt/<volume>/…`); the browser walks the volume roots of the
// file manager, whose paths are relative to the volume.

export const MOUNT_ROOT = '/mnt';

export const isISO = (name: string) => /\.iso$/i.test(name);

/** Path QEMU gets for a file in a folder of a volume. */
export function isoPath(volume: string, dir: string, name: string): string {
	return [MOUNT_ROOT, volume, ...dir.split('/').filter(Boolean), name].join('/');
}

/** Volume and folder of a path, to open the browser where a medium lies. */
export function isoLocation(
	path: string,
	volumes: readonly string[],
): { volume: string; dir: string } | null {
	const parts = path.split('/');
	if (parts[0] !== '' || `/${parts[1]}` !== MOUNT_ROOT || parts.includes('..')) return null;
	const volume = parts[2];
	if (!volume || !volumes.includes(volume) || parts.length < 4) return null;
	return { volume, dir: parts.slice(3, -1).filter(Boolean).join('/') };
}

/** Found media whose path contains every word of the query, in any case. */
export function matchMedia<T extends { path: string }>(media: readonly T[], query: string): T[] {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	return media.filter((entry) => {
		const path = entry.path.toLowerCase();
		return words.every((word) => path.includes(word));
	});
}
