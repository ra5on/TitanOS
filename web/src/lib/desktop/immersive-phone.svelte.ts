// Registered by a mounted reader or camera player. Two surfaces may overlap
// during navigation, so releasing the old one must keep the new one immersive.
export const phoneChrome = $state({
	immersive: 0,
	dock: false,
	customizeDock: false,
	customize: false,
});

export function immersivePhone(_node: HTMLElement) {
	phoneChrome.immersive++;
	return {
		destroy() {
			phoneChrome.immersive--;
		},
	};
}
