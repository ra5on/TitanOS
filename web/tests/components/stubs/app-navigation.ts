// Stand-in for $app/navigation: components under test do not navigate.
// Guards of beforeNavigate are kept, so a test can ask them (`tryNavigate`),
// and goto notes where it was asked to go (`gotoCalls`).
type Guard = (navigation: {
	to: { url: URL } | null;
	type: string;
	delta?: number;
	willUnload: boolean;
	cancel: () => void;
}) => void;
const guards: Guard[] = [];
export const gotoCalls: string[] = [];
export const goto = async (url: string | URL) => {
	gotoCalls.push(String(url));
};
export const beforeNavigate = (guard: Guard) => {
	guards.push(guard);
};
/** Runs the guards like a click on a link to `to`; true when one cancelled it. */
export function tryNavigate(to: string): boolean {
	let cancelled = false;
	for (const guard of guards)
		guard({
			to: { url: new URL(to, 'http://nas.test') },
			type: 'link',
			willUnload: false,
			cancel: () => (cancelled = true),
		});
	return cancelled;
}
export function forgetGuards() {
	guards.length = 0;
	gotoCalls.length = 0;
}
export const afterNavigate = () => {};
export const onNavigate = () => {};
export const invalidateAll = async () => {};
export const pushState = () => {};
export const replaceState = () => {};
