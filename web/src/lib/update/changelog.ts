// The release notes of an update (PLAN 78-3). They are the message of the
// release's tag, a small Markdown text:
//
//   # TitanOS 0.75 — the lead sentence
//   intro paragraphs
//   ## Neu            (also "Sicherheit", "Behoben", or anything else)
//   **Arbeitsplatz**  (a line that is only bold: a sub-heading of the bullets)
//   - a bullet, which may go on
//     in the next indented line, with **bold** and `code`
//
// This turns the text into data; the page shows it as text only (never as
// HTML). A link is kept as one only if it leads to the project's own pages.

/** A run of text inside a line. */
export type Part = { text: string; bold?: true; code?: true; href?: string };
/** One bullet with the bullets indented below it. */
export type Item = { parts: Part[]; children?: Item[] };
/** Paragraphs and the bullets after them, under an optional sub-heading. */
export type Group = { title: string; text: Part[][]; items: Item[] };
export type SectionKind = 'intro' | 'new' | 'security' | 'fixed' | 'other';
export type Section = {
	kind: SectionKind;
	/** The heading as written; empty for the intro. */
	title: string;
	groups: Group[];
	/** The bullets of the section, the indented ones below a bullet not counted. */
	count: number;
};
export type Changelog = {
	/** The first heading without the "#": "TitanOS 0.75 — Durchsicht …". */
	title: string;
	/** Its part after the dash, else empty. */
	lead: string;
	sections: Section[];
};

/** Links are shown as links only below this address. */
export const PROJECT_URL = 'https://github.com/ra5on/TitanOS';

/** Whether an address belongs to the project (and is not "…/TitanOS-evil"). */
export function projectLink(url: string): boolean {
	if (!url.startsWith(PROJECT_URL)) return false;
	const next = url.charAt(PROJECT_URL.length);
	return next === '' || next === '/' || next === '#' || next === '?';
}

/** The page of a release: the tag of 0.76 is v0.76.0. */
export function releaseUrl(version: string): string {
	const plain = version.trim().replace(/^v/, '');
	const tag = /^\d+\.\d+$/.test(plain) ? `${plain}.0` : plain;
	return `${PROJECT_URL}/releases/tag/v${encodeURIComponent(tag)}`;
}

const KINDS: Record<string, SectionKind> = {
	neu: 'new',
	new: 'new',
	sicherheit: 'security',
	security: 'security',
	behoben: 'fixed',
	fixed: 'fixed',
};

function kindOf(heading: string): SectionKind {
	return KINDS[heading.replace(/:$/, '').trim().toLowerCase()] ?? 'other';
}

const INLINE =
	/\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)|https:\/\/github\.com\/ra5on\/TitanOS[^\s<>()[\]"']*/g;

/** One line as its runs of plain, bold and code text and project links. */
export function inline(text: string, bold = false): Part[] {
	const parts: Part[] = [];
	const push = (part: Part) => {
		if (!part.text) return;
		const last = parts.at(-1);
		// Neighbouring plain runs are one run.
		if (last && !last.code && !last.href && !part.code && !part.href && last.bold === part.bold)
			last.text += part.text;
		else parts.push(part);
	};
	const plain = (value: string) => push(bold ? { text: value, bold: true } : { text: value });
	let at = 0;
	for (const match of text.matchAll(INLINE)) {
		const [all, strong, code, label, url] = match;
		plain(text.slice(at, match.index));
		at = match.index + all.length;
		if (strong !== undefined) {
			// Bold may hold code; bold inside bold does not exist.
			if (bold) plain(all);
			else for (const part of inline(strong, true)) push(part);
		} else if (code !== undefined) {
			push(bold ? { text: code, bold: true, code: true } : { text: code, code: true });
		} else if (label !== undefined) {
			// A link elsewhere stays the text that was written.
			if (projectLink(url)) push({ text: label, href: url, ...(bold ? { bold: true } : {}) });
			else plain(all);
		} else {
			// A bare address: the full stop of the sentence is not part of it.
			const address = all.replace(/[.,;:!?]+$/, '');
			if (projectLink(address)) {
				push({ text: address, href: address, ...(bold ? { bold: true } : {}) });
				plain(all.slice(address.length));
			} else plain(all);
		}
	}
	plain(text.slice(at));
	return parts;
}

/** The first line of the notes without the version: "Kernel 6.12.115, …". */
export function leadOf(changelog: string): string {
	const first = changelog.split('\n').find((line) => line.trim()) ?? '';
	const text = first.trim().replace(/^#+\s*/, '');
	const dash = text.indexOf(' — ');
	return dash >= 0 ? text.slice(dash + 3) : text;
}

export function parseChangelog(source: string): Changelog {
	const log: Changelog = { title: '', lead: '', sections: [] };
	// A signed tag carries its signature after the message.
	const body = source.replace(/\r\n?/g, '\n').split(/^-----BEGIN [A-Z ]+-----$/m)[0];
	let section: Section = { kind: 'intro', title: '', groups: [], count: 0 };
	type Open = { raw: string; into: Part[] };
	// (`null as …`: these are set inside the helpers below, which the type
	// checker's narrowing does not follow.)
	let group = null as Group | null;
	// What a following line may continue: a paragraph, or a bullet.
	let open = null as Open | null;
	let lastItem = null as Item | null;
	let blank = true;

	const closeSection = () => {
		section.groups = section.groups.filter((g) => g.title || g.text.length || g.items.length);
		if (section.groups.length) log.sections.push(section);
	};
	const newGroup = (title: string) => {
		group = { title, text: [], items: [] };
		section.groups.push(group);
		open = null;
		lastItem = null;
		return group;
	};
	const write = (raw: string, into: Part[]) => {
		into.length = 0;
		into.push(...inline(raw));
		open = { raw, into };
	};

	for (const line of body.split('\n')) {
		const text = line.trim();
		if (!text) {
			blank = true;
			continue;
		}
		const wasBlank = blank;
		blank = false;
		const indent = line.length - line.trimStart().length;
		const heading = /^(#{1,6})\s+(.*)$/.exec(text);
		if (heading) {
			const name = heading[2].replace(/\s+#+$/, '').trim();
			if (heading[1].length === 1 && !log.title && !log.sections.length && !section.groups.length) {
				log.title = name;
				const dash = /\s[—–]\s/.exec(name);
				log.lead = dash ? name.slice(dash.index + dash[0].length).trim() : '';
			} else if (heading[1].length <= 2) {
				closeSection();
				section = { kind: kindOf(name), title: name, groups: [], count: 0 };
				group = null;
			} else newGroup(name.replace(/^\*\*(.+)\*\*$/, '$1'));
			open = null;
			lastItem = null;
			continue;
		}
		// A line that is only bold names the bullets after it.
		const only = /^\*\*([^*]+)\*\*:?$/.exec(text);
		if (only) {
			newGroup(only[1].replace(/:$/, '').trim());
			continue;
		}
		if (/^([-*_])(\s*\1){2,}$/.test(text)) {
			// A rule between parts: nothing to show.
			open = null;
			continue;
		}
		const bullet = /^(?:[-*•]\s+|(\d+[.)])\s+)(.*)$/.exec(text);
		if (bullet) {
			// A numbered point keeps its number as text.
			const raw = bullet[1] ? `${bullet[1]} ${bullet[2]}` : bullet[2];
			const item: Item = { parts: [] };
			const parent = lastItem;
			if (indent >= 2 && parent) (parent.children ??= []).push(item);
			else {
				(group ?? newGroup('')).items.push(item);
				section.count++;
				lastItem = item;
			}
			write(raw, item.parts);
			continue;
		}
		const going = open;
		if (going && (!wasBlank || (indent >= 2 && lastItem))) {
			// The sentence goes on in this line.
			write(`${going.raw} ${text}`, going.into);
			continue;
		}
		// A paragraph. After bullets it starts a part of its own.
		let target: Group = group ?? newGroup('');
		if (target.items.length) target = newGroup('');
		const paragraph: Part[] = [];
		target.text.push(paragraph);
		lastItem = null;
		write(text, paragraph);
	}
	closeSection();
	return log;
}

/** How many bullets the notes have under Neu, Sicherheit and Behoben. */
export function counts(log: Changelog): { new: number; security: number; fixed: number } {
	const sum = { new: 0, security: 0, fixed: 0 };
	for (const section of log.sections)
		if (section.kind === 'new' || section.kind === 'security' || section.kind === 'fixed')
			sum[section.kind] += section.count;
	return sum;
}

/** A group cut to the bullets that are still to show. */
export type Shown = { group: Group; items: Item[] };

/**
 * The first `limit` bullets of a section, in their groups. A group whose
 * bullets all fall behind the limit is left out with its heading and text.
 */
export function firstItems(section: Section, limit: number): Shown[] {
	const shown: Shown[] = [];
	let left = limit;
	for (const group of section.groups) {
		if (group.items.length === 0) {
			// Text without bullets belongs to what is shown before the limit is reached.
			if (left > 0 || limit <= 0) shown.push({ group, items: [] });
			continue;
		}
		if (left <= 0) break;
		const items = group.items.slice(0, left);
		left -= items.length;
		shown.push({ group, items });
	}
	return shown;
}
