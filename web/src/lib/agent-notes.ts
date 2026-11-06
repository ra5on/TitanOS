// Notes of the KI-Agent (PLAN 57): paths and the format bar. No runes, so
// it can be tested with node --test.

export type NoteInfo = {
	path: string;
	title: string;
	folder: string;
	modified: string;
	size: number;
	pinned?: boolean;
	preview?: string;
};

/** A name as the backend takes it: no slashes, no leading dot. */
export function noteName(name: string): string {
	return name
		.replace(/[\\/<>:"|?*\u0000-\u001f]/g, '-')
		.replace(/^\.+/, '')
		.trim()
		.slice(0, 117);
}

/** The path of a note in a folder ('' the top). */
export function notePath(folder: string, title: string): string {
	const name = noteName(title) || 'Notiz';
	return (folder ? folder + '/' : '') + name + '.md';
}

/**
 * The new path when a note or folder gets a new name in a folder: a note
 * keeps .md, a folder never gets it. `_from` is the old path (unused, for
 * the call sites to read well).
 */
export function noteMove(_from: string, parent: string, name: string, isFolder: boolean): string {
	const clean = noteName(name.replace(/\.md$/i, '')) || (isFolder ? 'Ordner' : 'Notiz');
	return (parent ? parent + '/' : '') + clean + (isFolder ? '' : '.md');
}

export type Format = 'heading' | 'bold' | 'italic' | 'list' | 'check' | 'rule';

/**
 * Applies a button of the format bar to the text and selection: bold and
 * italic wrap the selection (or insert markers to type into), heading,
 * list and checklist switch the selected lines, the rule adds a line.
 * Returns the new text and selection.
 */
export function applyFormat(
	text: string,
	start: number,
	end: number,
	f: Format,
): { text: string; start: number; end: number } {
	if (f === 'bold' || f === 'italic') {
		const mark = f === 'bold' ? '**' : '_';
		const inner = text.slice(start, end);
		const out = text.slice(0, start) + mark + inner + mark + text.slice(end);
		return { text: out, start: start + mark.length, end: end + mark.length };
	}
	if (f === 'rule') {
		const before = text.slice(0, start);
		const pre = before === '' || before.endsWith('\n') ? '' : '\n';
		const ins = pre + '\n---\n\n';
		const out = before + ins + text.slice(end);
		const at = start + ins.length;
		return { text: out, start: at, end: at };
	}
	const lineStart = text.lastIndexOf('\n', start - 1) + 1;
	let lineEnd = text.indexOf(
		'\n',
		Math.max(end - (end > start && text[end - 1] === '\n' ? 1 : 0), start),
	);
	if (lineEnd < 0) lineEnd = text.length;
	const lines = text.slice(lineStart, lineEnd).split('\n');
	const marker = f === 'heading' ? '## ' : f === 'list' ? '- ' : '- [ ] ';
	const has = (l: string) =>
		f === 'heading'
			? /^#{1,6} /.test(l)
			: f === 'list'
				? /^[-*] (?!\[[ xX]\] )/.test(l)
				: /^[-*] \[[ xX]\] /.test(l);
	const strip = (l: string) => l.replace(/^#{1,6} /, '').replace(/^[-*] (\[[ xX]\] )?/, '');
	const all = lines.every((l) => l.trim() === '' || has(l));
	const changed = lines.map((l) =>
		all ? strip(l) : l.trim() === '' && lines.length > 1 ? l : marker + strip(l),
	);
	const block = changed.join('\n');
	const out = text.slice(0, lineStart) + block + text.slice(lineEnd);
	const delta = block.length - (lineEnd - lineStart);
	const caret = lines.length === 1 && start === end ? lineStart + block.length : start;
	return {
		text: out,
		start: lines.length === 1 && start === end ? caret : lineStart,
		end: lines.length === 1 && start === end ? caret : lineEnd + delta,
	};
}
