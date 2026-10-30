// Maintenance and monitoring report their problems as English sentences of
// the backend. Known ones are translated; for the rest a translated summary
// is shown and the original text stays visible as technical detail.
import { ApiError } from '#lib/api.ts';
import { errorText, type MessageKey } from '#lib/i18n/index.svelte.ts';

export type Problem = { text: string; detail: string };

const KNOWN: [RegExp, MessageKey][] = [
	[/^previous execution outcome is unknown/i, 'ops.err.unknownOutcome'],
	[/^could not persist execution outcome/i, 'ops.err.persist'],
	[/disabled in development mode/i, 'ops.err.devMode'],
	[/^maintenance unavailable|^service unavailable/i, 'ops.err.unavailable'],
	[/^maintenance volume changed/i, 'ops.err.volumeChanged'],
	[/^(another maintenance job|maintenance) is running/i, 'ops.err.running'],
	[/^waiting/i, 'ops.err.waiting'],
	[/dependency cycle/i, 'ops.err.cycle'],
	[/dependency is missing/i, 'ops.err.dependencyMissing'],
	[/^review the interrupted job/i, 'ops.err.reviewFirst'],
	[/^choose weekdays/i, 'ops.err.weekdays'],
	[/^snapshot retention must be confirmed/i, 'ops.err.pruneConsent'],
	[/^(cancelled|canceled|context canceled)$/i, 'ops.err.cancelled'],
	[/budget exhausted|exceeds budget|exceeds limit/i, 'ops.err.budget'],
	[/too many observation gaps/i, 'ops.err.gaps'],
	[/^invalid /i, 'ops.err.invalid'],
];

/** A translated text for a problem text of the backend. */
export function problemText(raw: string, translate: (key: MessageKey) => string): Problem {
	const message = raw.trim();
	for (const [pattern, key] of KNOWN) {
		if (pattern.test(message)) {
			// Waiting reasons and persistence errors carry useful detail.
			const keep =
				key === 'ops.err.waiting' || key === 'ops.err.persist' || key === 'ops.err.invalid';
			return { text: translate(key), detail: keep ? message : '' };
		}
	}
	return { text: translate('ops.err.failed'), detail: message };
}

/** A translated text for a failed request. */
export function requestProblem(err: unknown, translate: (key: MessageKey) => string): Problem {
	if (
		err instanceof ApiError &&
		!['operation_failed', 'bad_request', 'unavailable'].includes(err.code)
	)
		return { text: errorText(err.code), detail: '' };
	return problemText(err instanceof Error ? err.message : String(err), translate);
}
