// §121 (WebKit for the home screen app): the screen stays on while
// something needs looking at – a VM's screen, the NVR's live view, files
// being uploaded. Screen Wake Lock where the browser has it (Safari 16.4 and
// newer, Chrome); a lock the system took back when the page was hidden is
// asked for again when it shows. Elsewhere nothing happens.
type Lock = { release: () => Promise<void> };
type WakeNavigator = Navigator & { wakeLock?: { request: (type: 'screen') => Promise<Lock> } };

/** Call during a component's setup; `on` says whether the screen should stay on right now. */
export function keepAwake(on: () => boolean) {
	$effect(() => {
		const nav = navigator as WakeNavigator;
		if (!on() || !nav.wakeLock) return;
		let lock: Lock | null = null;
		let gone = false;
		const ask = () => {
			if (gone || document.visibilityState !== 'visible') return;
			nav
				.wakeLock!.request('screen')
				.then((l) => (gone ? void l.release().catch(() => {}) : (lock = l)))
				.catch(() => {});
		};
		ask();
		document.addEventListener('visibilitychange', ask);
		return () => {
			gone = true;
			document.removeEventListener('visibilitychange', ask);
			void lock?.release().catch(() => {});
		};
	});
}
