// Small helpers shared by the tabs of Settings › IPv64.
import { ApiError } from '#lib/api.ts';
import { formatDateTime } from '#lib/format.ts';
import { errorText } from '#lib/i18n/index.svelte.ts';

/** An error for people: the reason of IPv64 is appended where there is one. */
export function failure(err: unknown): string {
	if (err instanceof ApiError && err.code.startsWith('ipv64_') && err.code !== 'ipv64_no_key') {
		return `${errorText(err.code)} ${err.message.replace(/^IPv64: (HTTP \d+: )?/, '')}`;
	}
	return errorText(err instanceof ApiError ? err.code : 'internal');
}

export function when(value?: string | null): string {
	return value ? formatDateTime(new Date(value)) : '–';
}

/** Runs an action with a busy flag and an error text. */
export async function attempt(set: (busy: boolean, error: string) => void, action: () => Promise<unknown>): Promise<boolean> {
	set(true, '');
	try {
		await action();
		set(false, '');
		return true;
	} catch (err) {
		set(false, failure(err));
		return false;
	}
}
