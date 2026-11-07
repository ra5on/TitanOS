// PLAN 65: the navigation of the classic frame (head line or side bar) is
// gone with the frame. Its stored settings ("nav" in the appearance) stay
// readable and are written back unchanged, so that older and newer versions
// sharing /config keep them.

/** Older versions never hide the settings tab. */
export const NAV_FIXED = 'settings';

/**
 * Own order and hidden tabs of an account; empty = standard. `layout`: header
 * (top) or side bar; `magnify`: symbols of the folded side bar under the pointer.
 */
export type NavPrefs = {
	order: string[];
	hidden: string[];
	layout: NavLayout;
	magnify: boolean;
	/** Corner of the menu button with the side bar on a phone (PLAN 53). */
	button: NavButton;
};
export type NavLayout = 'top' | 'side';
export const NAV_BUTTONS = ['tl', 'tr', 'bl', 'br'] as const;
export type NavButton = (typeof NAV_BUTTONS)[number];

export const defaultNav = (): NavPrefs => ({
	order: [],
	hidden: [],
	layout: 'top',
	magnify: true,
	button: 'tl',
});

// Unknown ids are kept, so a tab of a newer app version keeps its place
// when an older version saves the appearance in between.
const ID = /^[a-z][a-z0-9-]{0,31}$/;

function ids(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return [
		...new Set(value.filter((id): id is string => typeof id === 'string' && ID.test(id))),
	].slice(0, 64);
}

export function sanitizeNav(input: unknown): NavPrefs {
	if (typeof input !== 'object' || input === null) return defaultNav();
	const v = input as Record<string, unknown>;
	return {
		order: ids(v.order),
		hidden: ids(v.hidden).filter((id) => id !== NAV_FIXED),
		layout: v.layout === 'side' ? 'side' : 'top',
		magnify: v.magnify !== false,
		button: NAV_BUTTONS.includes(v.button as NavButton) ? (v.button as NavButton) : 'tl',
	};
}
