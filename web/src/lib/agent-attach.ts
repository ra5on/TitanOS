// Names of the files attached to a message of the KI-Agent (1.01). Two
// files of the same name are both kept: on the iPhone every photo is
// "image.jpg", and the second one used to be dropped without a word.

/** "stem (n).ext" for the n-th file of a name; the name itself for n = 1. */
export function numberedName(name: string, n: number): string {
	if (n <= 1) return name;
	const dot = name.lastIndexOf('.');
	const ext = dot > 0 && name.length - dot <= 20 ? name.slice(dot) : '';
	return `${name.slice(0, name.length - ext.length)} (${n})${ext}`;
}

/** The name a new file gets beside the ones already taken. */
export function freeName(name: string, taken: readonly string[]): string {
	for (let n = 1; n < 1000; n++) {
		const next = numberedName(name, n);
		if (!taken.includes(next)) return next;
	}
	return name;
}

/** The same file chosen twice (same name, size and time) is added once. */
export function sameFile(a: File, b: File): boolean {
	return a.name === b.name && a.size === b.size && a.lastModified === b.lastModified;
}
