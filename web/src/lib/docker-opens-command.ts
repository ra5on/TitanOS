// PLAN 72: the program line of „Öffnen mit“ für Docker-Apps and the shares
// it mounted; plain functions without the session (tested on their own).

export const SHARES_TARGET = '/shares/';

/**
 * Splits the program line of the editor into arguments: words apart by
 * spaces, "…" keeps spaces inside one argument. No shell: nothing else is
 * special.
 */
export function splitCommand(line: string): string[] {
	const out: string[] = [];
	const re = /"([^"]*)"|(\S+)/g;
	for (const m of line.matchAll(re)) out.push(m[1] ?? m[2]);
	return out;
}

/** The program line for the editor (arguments with spaces in quotes). */
export function joinCommand(args: string[]): string {
	return args.map((a) => (/\s/.test(a) || a === '' ? `"${a}"` : a)).join(' ');
}

/** The shares mounted by „Öffnen mit“ (target /shares/<name>). */
export function mountedShares(opens: {
	mounts: { source: string; target: string; readOnly: boolean }[];
}): { share: string; readOnly: boolean }[] {
	return opens.mounts
		.filter((m) => m.target.startsWith(SHARES_TARGET) && !m.target.slice(8).includes('/'))
		.map((m) => ({ share: m.target.slice(SHARES_TARGET.length), readOnly: m.readOnly }));
}
