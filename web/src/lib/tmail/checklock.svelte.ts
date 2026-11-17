// 1.05: titand allows at most 10 connection checks per minute and account
// (mail_rate_limited, 429, Retry-After). "Verbindung prüfen" then stays off
// until the time is over and says why, in the assistant and at a mailbox.
import { ApiError } from '../api.ts';

export const checkLock = $state({ until: 0, now: Date.now() });

let timer: ReturnType<typeof setInterval> | undefined;

/** Locks the checks after a mail_rate_limited answer; true when it was one. */
export function lockFrom(err: unknown): boolean {
	if (!(err instanceof ApiError) || err.code !== 'mail_rate_limited') return false;
	checkLock.now = Date.now();
	checkLock.until = checkLock.now + (err.retryAfter > 0 ? err.retryAfter : 60) * 1000;
	clearInterval(timer);
	timer = setInterval(() => {
		checkLock.now = Date.now();
		if (checkLock.now >= checkLock.until) {
			clearInterval(timer);
			checkLock.until = 0;
		}
	}, 1000);
	return true;
}

/** Seconds until checks are allowed again; 0 when they are. */
export function lockedSeconds(): number {
	return checkLock.until ? Math.max(0, Math.ceil((checkLock.until - checkLock.now) / 1000)) : 0;
}
