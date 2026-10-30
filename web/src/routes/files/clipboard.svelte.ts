// Copy, cut and paste inside the file manager. Only the paths are kept;
// pasting opens the copy dialog with them.

import type { FileRoot } from '#lib/api.ts';

export type ClipboardContent = { from: FileRoot; paths: string[]; move: boolean };

export class FileClipboard {
	content = $state<ClipboardContent | null>(null);

	/** Remembers entries of `dir` in `from`; `move` for cut. */
	remember(from: FileRoot, dir: string, names: string[], move: boolean) {
		this.content = {
			from,
			paths: names.map((name) => [dir, name].filter(Boolean).join('/')),
			move,
		};
	}

	/** Whether the content may go to a writable folder; a cut also needs its source writable. */
	canPaste(writable: boolean, roots: FileRoot[]) {
		const content = this.content;
		return (
			!!content &&
			writable &&
			(!content.move || !!roots.find((place) => place.id === content.from.id)?.writable)
		);
	}

	/** Forgets a cut; a copy stays. */
	dropCut() {
		if (this.content?.move) this.content = null;
	}
}
