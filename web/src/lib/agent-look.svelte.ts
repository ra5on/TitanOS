// The look of the KI-Agent outside its own page: the desktop widget shows
// the same working figure as the chat, in the colour and size chosen under
// KI-Agent › Einstellungen › Darstellung (owner, 1.06).
//
// The agent page keeps a copy of the look in this browser (LOOK_KEY) and
// rewrites it on every change. That copy is read at once, and followed
// through `storage` events: an agent window of the desktop is a document of
// its own, so its changes arrive here as such events. The account's look on
// the NAS (/agent/prefs) is asked for only when a figure is shown and this
// browser has no copy, at most once per page load – a desktop that loads
// does not ask for it.
import { api } from './api.ts';
import { defaultLook, LOOK_KEY, sanitizeLook, type Look } from './agent.ts';

class SharedLook {
	value = $state<Look>(defaultLook());
	#started = false;
	#local = false;
	#asked = false;

	#read() {
		try {
			const raw = localStorage.getItem(LOOK_KEY);
			if (raw === null) return;
			this.value = sanitizeLook(JSON.parse(raw));
			this.#local = true;
		} catch {
			// No storage or no valid copy: the defaults stay.
		}
	}

	/** Reads this browser's copy and follows its changes. */
	start() {
		if (this.#started || typeof window === 'undefined') return;
		this.#started = true;
		this.#read();
		window.addEventListener('storage', (event) => {
			if (event.key === LOOK_KEY) this.#read();
		});
	}

	/** Called when a figure is shown: without a copy in this browser, the
	 *  account's look is asked for once. */
	need() {
		this.start();
		if (this.#local || this.#asked) return;
		this.#asked = true;
		api
			.get<Record<string, unknown>>('/agent/prefs')
			.then((saved) => {
				if (!this.#local && saved && Object.keys(saved).length)
					this.value = sanitizeLook({ ...this.value, ...saved });
			})
			.catch(() => {});
	}

	/** For tests: forget what was read and asked. */
	reset() {
		this.value = defaultLook();
		this.#started = this.#local = this.#asked = false;
	}
}

export const agentLook = new SharedLook();

/** Whether the figure stands still: "Animationen" not full, or calm. The
 *  figure itself also stands still with "less motion" of the system. */
export function lookCalm(look: Look): boolean {
	return look.calm || look.motion !== 'full';
}
