// PLAN 86: a hit of the settings search leads to the setting itself. The
// address names the page, its tab (`?tab=`) and the place: an anchor
// (`#access-ssh`) or the titles of the groups and rows around it and its
// own (`#find=Farben%20und%20Schrift|Sprache`, see findHash). After the
// page has drawn – its data may come later – the tab is chosen, every row
// on the way is opened, and the setting is scrolled into view, marked for
// a moment and – when the search is on the same page – given the keyboard.
//
// It works on what the page shows (SettingGroup, SettingRow, SettingSub and
// Tabs), so no page needs code of its own for it.

const WAIT_MS = 6000;
const MARK_MS = 4000;
/** How long after the jump began its control may still take the keyboard. */
const KEYS_MS = 2000;

/** The titles of a "find=" anchor, or null for a plain one. */
export function findTitles(hash: string): string[] | null {
	const raw = hash.replace(/^#/, '');
	if (!raw.startsWith('find=')) return null;
	return raw
		.slice(5)
		.split('|')
		.map((part) => {
			try {
				return decodeURIComponent(part);
			} catch {
				return part;
			}
		})
		.filter(Boolean);
}

const squash = (text: string) => text.replace(/\s+/g, ' ').trim();

/** The title of a group, row or part – its own text, without a badge. */
function ownTitle(el: Element): string {
	const title = el.matches('.sgroup')
		? el.querySelector(':scope > .sgroup-head > .sgroup-title')
		: el.matches('.srow-sub')
			? el.querySelector(':scope > .srow-text > .srow-title')
			: el.querySelector(':scope > .srow-head .srow-title');
	if (!title) return '';
	let text = '';
	for (const node of title.childNodes)
		if (node.nodeType === Node.TEXT_NODE) text += node.textContent;
	return squash(text);
}

/** The first group, row or part inside `scope` with this title. */
function findIn(scope: ParentNode, title: string): HTMLElement | null {
	const wanted = squash(title);
	for (const el of scope.querySelectorAll<HTMLElement>('.sgroup, .srow, .srow-sub'))
		if (ownTitle(el) === wanted) return el;
	return null;
}

/** Opens a closed row (SettingRow listens for this event; nothing is remembered). */
function openRow(el: HTMLElement): boolean {
	if (!el.matches('.srow') || el.classList.contains('open')) return false;
	if (!el.querySelector(':scope > .srow-head > .srow-chev[aria-expanded]')) return false;
	el.dispatchEvent(new CustomEvent('titanos-open'));
	return true;
}

/**
 * Chooses the tab of `?tab=` if the page shows tabs and another is chosen.
 * True once done (or nothing to do): a jump chooses the tab once, never
 * again over a tab the user picked while it waits for a row.
 */
function chooseTab(root: ParentNode, tab: string | null): boolean {
	if (!tab) return true;
	const button = root.querySelector<HTMLElement>(
		`[role="tablist"] [role="tab"][data-tab="${CSS.escape(tab)}"]`,
	);
	if (!button) return false;
	if (button.getAttribute('aria-selected') !== 'true') button.click();
	return true;
}

let marked: HTMLElement | null = null;
let unmark: ReturnType<typeof setTimeout> | undefined;
/**
 * Marks the place; `keys` gives its control the keyboard. That happens only
 * when this page had the keyboard when the jump began (the search on the
 * same page: phone, own tab) and the place came quickly: a search in a
 * window's side bar keeps its keyboard, and a row that appears seconds
 * later must not pull the keyboard away from what the user does meanwhile.
 */
function mark(el: HTMLElement, keys = false) {
	marked?.classList.remove('found');
	clearTimeout(unmark);
	marked = el;
	el.classList.add('found');
	unmark = setTimeout(() => el.classList.remove('found'), MARK_MS);
	const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
	el.scrollIntoView({ block: 'center', behavior: still ? 'auto' : 'smooth' });
	// The keyboard goes to the setting's own control, if it has one.
	const control = el.querySelector<HTMLElement>(
		':scope > .srow-head .srow-ctl :is(input, select, textarea, button, a[href]), :scope > .srow-ctl :is(input, select, textarea, button, a[href]), :scope > .srow-head > .srow-chev',
	);
	if (keys) control?.focus({ preventScroll: true });
}

let run = 0;
const frame = () => new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Leads to the place the address names (call after each navigation). A new
 * call ends the one before. Resolves with the element marked, if any.
 */
export async function jumpToSetting(
	url: { hash: string; searchParams: { get(name: string): string | null } },
	root: ParentNode = document,
): Promise<HTMLElement | null> {
	const mine = ++run;
	const begun = Date.now();
	const hadKeys = document.hasFocus();
	const keys = () => hadKeys && document.hasFocus() && Date.now() - begun < KEYS_MS;
	const titles = findTitles(url.hash);
	const id = !titles && url.hash.length > 1 ? decodeURIComponent(url.hash.slice(1)) : '';
	const tab = url.searchParams.get('tab');
	if (!titles && !id && !tab) return null;
	const until = Date.now() + WAIT_MS;
	let deepest: HTMLElement | null = null;
	// The page of the new address draws first.
	await frame();
	if (mine !== run) return null;
	let tabDone = false;
	while (mine === run && Date.now() < until) {
		if (!tabDone) tabDone = chooseTab(root, tab);
		if (!titles && !id) return null;
		if (id) {
			const el = document.getElementById(id);
			if (el) {
				await frame();
				if (mine !== run) return null;
				mark(el, keys());
				return el;
			}
		} else if (titles) {
			let scope: ParentNode = root;
			let found = 0;
			let waiting = false;
			for (const [i, title] of titles.entries()) {
				const el = findIn(scope, title);
				if (!el) break;
				found = i + 1;
				deepest = el;
				// Rows on the way open; the row itself opens too, to show its parts.
				if (openRow(el)) waiting = true;
				if (waiting) break;
				scope = el;
			}
			if (found === titles.length && !waiting) {
				if (deepest) mark(deepest, keys());
				return deepest;
			}
		}
		await pause(100);
		await frame();
	}
	// The page never showed it (a part that appears only with data): the
	// nearest place found instead.
	if (mine === run && deepest) mark(deepest, keys());
	return mine === run ? deepest : null;
}
