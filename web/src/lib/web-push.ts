// Notifications on this device over Web Push (docs/WEB-PUSH.md): the
// service worker /sw.js shows what the NAS sends; this module registers it
// and subscribes the browser with the key of the NAS.
import { api } from './api.ts';

export type PushDevice = { id: string; name: string; service: string; created: string };

/** Registers the service worker where browsers allow one (HTTPS or localhost). */
export function registerServiceWorker(): void {
	if (typeof navigator === 'undefined' || !('serviceWorker' in navigator) || !window.isSecureContext) return;
	navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => {
		// Without it there are no notifications on this device; nothing else depends on it.
	});
}

/** Whether this browser can receive Web Push at all. */
export function pushSupported(): boolean {
	return typeof window !== 'undefined' && window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
}

/** An iPhone or iPad: Web Push only works from the home screen there. */
export function appleMobile(userAgent: string, touchPoints: number): boolean {
	return /iPhone|iPad|iPod/.test(userAgent) || (/Macintosh/.test(userAgent) && touchPoints > 1);
}

/** Whether TitanOS runs as an app from the home screen. */
export function standalone(): boolean {
	return window.matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true;
}

/** A name for this device to tell it apart in the list. */
export function deviceName(userAgent: string, touchPoints: number): string {
	if (/iPhone/.test(userAgent)) return 'iPhone';
	if (/iPad/.test(userAgent) || (/Macintosh/.test(userAgent) && touchPoints > 1)) return 'iPad';
	if (/Android/.test(userAgent)) return 'Android';
	const browser = /Edg\//.test(userAgent) ? 'Edge' : /Firefox\//.test(userAgent) ? 'Firefox' : /Chrome\//.test(userAgent) ? 'Chrome' : /Safari\//.test(userAgent) ? 'Safari' : 'Browser';
	const system = /Windows/.test(userAgent) ? 'Windows' : /Mac OS X|Macintosh/.test(userAgent) ? 'macOS' : /Linux/.test(userAgent) ? 'Linux' : '';
	return system ? `${browser} (${system})` : browser;
}

/** The VAPID key as applicationServerKey wants it. */
export function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
	const padded = base64url.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(base64url.length / 4) * 4, '=');
	const raw = atob(padded);
	const out = new Uint8Array(new ArrayBuffer(raw.length));
	for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
	return out;
}

/** The subscription of this browser, if it has one. */
export async function currentSubscription(): Promise<PushSubscription | null> {
	if (!pushSupported()) return null;
	const registration = await navigator.serviceWorker.getRegistration('/');
	return (await registration?.pushManager.getSubscription()) ?? null;
}

/** Asks for permission, subscribes this browser and tells the NAS. */
export async function subscribe(publicKey: string, name: string): Promise<PushDevice> {
	if ((await Notification.requestPermission()) !== 'granted') throw new Error('denied');
	const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
	await navigator.serviceWorker.ready;
	let subscription = await registration.pushManager.getSubscription();
	if (subscription) {
		// A subscription made with another key (another NAS) cannot be used.
		const key = subscription.options.applicationServerKey;
		const same = key && btoa(String.fromCharCode(...new Uint8Array(key))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') === publicKey;
		if (!same) {
			await subscription.unsubscribe();
			subscription = null;
		}
	}
	subscription ??= await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) });
	return api.post<PushDevice>('/notifications/push', { name, subscription: subscription.toJSON() });
}
