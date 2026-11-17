// 1.05: the draft of a new e-mail is saved every 30 s while it changes
// (docs/TMAIL.md, "Schreiben"). Nothing is sent while nothing changed, and
// never two saves at once; a change during a save waits for the next turn.

export const DRAFT_EVERY_MS = 30_000;

export type Autosave = {
	/** Something in the form changed. */
	touch(): void;
	/** Saves now if something changed (before closing, say). */
	flush(): Promise<void>;
	/** Whether changes wait for the next save. */
	dirty(): boolean;
	stop(): void;
};

export function createAutosave(save: () => Promise<void>, every = DRAFT_EVERY_MS): Autosave {
	let changed = false;
	let running: Promise<void> | null = null;

	async function run() {
		if (running) return running;
		if (!changed) return;
		changed = false;
		running = save()
			.catch(() => {
				// Not saved: the change counts again for the next turn.
				changed = true;
			})
			.finally(() => {
				running = null;
			});
		return running;
	}

	const timer = setInterval(() => void run(), every);
	return {
		touch: () => {
			changed = true;
		},
		flush: run,
		dirty: () => changed,
		stop: () => clearInterval(timer),
	};
}
