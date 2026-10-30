// Shared helper for pages that ask the API again and again.
//
// It replaces the copied block of "disposed", "inflight", "document.hidden",
// setInterval and cleanup. Use it straight from an effect or onMount, the
// returned function stops it:
//
//   $effect(() => poll(load, { interval: 5000 }));
//
// The run is started untracked: state that `run` reads synchronously (e.g. a
// "refreshing" flag) does not become a dependency of the calling effect. That
// is what made an effect start itself again after every answer.

import { untrack } from 'svelte';

export type PollOptions = {
	/** Milliseconds between the end of one run and the next; a function is asked again each time. */
	interval: number | (() => number);
	/** Run once right away (default true). */
	immediate?: boolean;
	/** Skip runs while the browser tab is hidden and catch up when it is shown again (default true). */
	pauseHidden?: boolean;
	/** Skip a scheduled run while this returns true, e.g. while the page saves. */
	skip?: () => boolean;
	/** Called with errors that `run` throws while the poller is running. */
	onError?: (err: unknown) => void;
};

export type Poller = (() => void) & {
	/** Runs now, regardless of the tab and `skip`. If a run is under way, one more follows it. */
	now: () => Promise<void>;
};

function hidden() {
	return typeof document !== 'undefined' && document.hidden;
}

/** Starts calling `run` regularly; the result stops it. Runs never overlap. */
export function poll(run: (signal: AbortSignal) => unknown, options: PollOptions): Poller {
	const { immediate = true, pauseHidden = true } = options;
	const controller = new AbortController();
	let stopped = false;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let current: Promise<void> | null = null;
	let again = false;
	let missed = false;

	function delay() {
		return typeof options.interval === 'function' ? untrack(options.interval) : options.interval;
	}

	function schedule() {
		if (stopped) return;
		clearTimeout(timer);
		timer = setTimeout(() => void scheduled(), delay());
	}

	async function scheduled() {
		if (pauseHidden && hidden()) {
			missed = true;
		} else if (!current && !(options.skip && untrack(options.skip))) {
			await start();
		}
		schedule();
	}

	function start(): Promise<void> {
		if (stopped) return Promise.resolve();
		if (current) {
			again = true;
			return current;
		}
		missed = false;
		current = (async () => {
			try {
				await untrack(() => run(controller.signal));
			} catch (err) {
				if (!stopped) options.onError?.(err);
			} finally {
				current = null;
			}
			if (again && !stopped) {
				again = false;
				await start();
			}
		})();
		return current;
	}

	function visible() {
		if (!hidden() && missed && !stopped) {
			void start();
			schedule();
		}
	}

	if (pauseHidden && typeof document !== 'undefined') {
		document.addEventListener('visibilitychange', visible);
	}
	if (immediate) void start();
	schedule();

	const stop = () => {
		if (stopped) return;
		stopped = true;
		clearTimeout(timer);
		controller.abort();
		if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', visible);
	};
	return Object.assign(stop, { now: start });
}
