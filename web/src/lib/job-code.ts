// 1.03: jobs of Docker and LXC may name a known cause of their end
// (`job.code`, from titand 1.03 on): "timeout" when docker compose was
// stopped at its time limit, "interrupted" when titand stopped while a
// container was being created; from 1.04 on for LXC also "timeout"
// (lxc-create ran over its 30 minutes) and "delete_failed" (removing the
// container with its data failed; it stays listed). With a text for the code the page shows it
// instead of titand's English line; older versions send no code and
// everything stays as it was. Pure (no runes): the caller passes how to
// look a text up, '' for none (`(key) => tOr(key, '')`).

export type TextLookup = (key: string) => string;

/** The key of the text for a job code of `area`, or '' for no usable code. */
export function jobCodeKey(area: 'docker' | 'lxc', code: unknown): string {
	return typeof code === 'string' && /^[a-z_]+$/.test(code) ? `${area}.jobCode.${code}` : '';
}

/** The text for a job code, or '' when there is none. */
export function jobCodeText(area: 'docker' | 'lxc', code: unknown, lookup: TextLookup): string {
	const key = jobCodeKey(area, code);
	return key ? lookup(key) : '';
}

/** A Docker job's output, its last line (titand's English reason) in the page's language. */
export function dockerJobOutput(
	job: { output?: string; code?: string },
	lookup: TextLookup,
): string {
	const output = job.output ?? '';
	const text = jobCodeText('docker', job.code, lookup);
	if (!text) return output;
	const lines = output.replace(/\s+$/, '').split('\n');
	lines[lines.length - 1] = text;
	return lines.join('\n');
}

/** An LXC job's text: the text for its code, else what titand wrote. */
export function lxcJobText(job: { text?: string; code?: string }, lookup: TextLookup): string {
	return jobCodeText('lxc', job.code, lookup) || job.text || '';
}

/** Whether a failed LXC job was the creation (the page's hint "create it anew"); not a failed removal. */
export function lxcCreateFailed(job: { code?: string }): boolean {
	return job.code !== 'delete_failed';
}
