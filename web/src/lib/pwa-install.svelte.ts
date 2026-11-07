// PLAN 70: installing TitanOS as an app. Chrome/Edge on Android and desktop
// offer it through "beforeinstallprompt" (caught as early as possible, it
// fires once at load); Safari on iOS only through "Zum Home-Bildschirm".

type InstallPrompt = Event & {
	prompt: () => Promise<void>;
	userChoice: Promise<{ outcome: string }>;
};

export const install = $state<{ prompt: InstallPrompt | null; installed: boolean }>({
	prompt: null,
	installed: false,
});

if (typeof window !== 'undefined') {
	install.installed =
		window.matchMedia?.('(display-mode: standalone)').matches ||
		(navigator as Navigator & { standalone?: boolean }).standalone === true;
	window.addEventListener('beforeinstallprompt', (event) => {
		event.preventDefault();
		install.prompt = event as InstallPrompt;
	});
	window.addEventListener('appinstalled', () => {
		install.installed = true;
		install.prompt = null;
	});
}

export function isIOS(): boolean {
	if (typeof navigator === 'undefined') return false;
	return (
		/iPhone|iPad|iPod/.test(navigator.userAgent) ||
		(navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
	);
}

/** Shows the browser's install question; true when the app was installed. */
export async function promptInstall(): Promise<boolean> {
	const p = install.prompt;
	if (!p) return false;
	install.prompt = null;
	await p.prompt();
	const choice = await p.userChoice.catch(() => ({ outcome: 'dismissed' }));
	if (choice.outcome === 'accepted') install.installed = true;
	return install.installed;
}
