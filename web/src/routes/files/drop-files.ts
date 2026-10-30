// Reading files and folders dropped from the desktop onto the file manager.

export type PickedFile = { file: File; path: string };

/** Reads a dropped folder with everything below it. What the browser cannot read is noted in `failed`. */
async function readEntry(
	entry: FileSystemEntry,
	prefix: string,
	out: PickedFile[],
	failed: string[],
) {
	try {
		if (entry.isFile) {
			const file = await new Promise<File>((resolve, reject) =>
				(entry as FileSystemFileEntry).file(resolve, reject),
			);
			out.push({ file, path: prefix + entry.name });
		} else if (entry.isDirectory) {
			const reader = (entry as FileSystemDirectoryEntry).createReader();
			for (;;) {
				const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
					reader.readEntries(resolve, reject),
				);
				if (batch.length === 0) break;
				for (const child of batch) await readEntry(child, prefix + entry.name + '/', out, failed);
			}
		}
	} catch {
		failed.push(prefix + entry.name);
	}
}

/** Everything that was dropped, folders with their contents; `failed` names what could not be read. */
export async function droppedFiles(
	data: DataTransfer,
): Promise<{ files: PickedFile[]; failed: string[] }> {
	// The entries have to be taken before the first await; the list is gone afterwards.
	const items = [...data.items]
		.map((item) => item.webkitGetAsEntry?.())
		.filter((entry): entry is FileSystemEntry => !!entry);
	const plain = [...data.files];
	const files: PickedFile[] = [],
		failed: string[] = [];
	if (items.length > 0) for (const entry of items) await readEntry(entry, '', files, failed);
	else files.push(...plain.map((file) => ({ file, path: file.name })));
	return { files, failed };
}

/** Files chosen with a file input, folders with their relative path. Empties the input. */
export function pickedFiles(input: HTMLInputElement): PickedFile[] {
	const files = [...(input.files ?? [])].map((file) => ({
		file,
		path: file.webkitRelativePath || file.name,
	}));
	input.value = '';
	return files;
}
