// The list of networks SSH logins may come from, as the access page edits it:
// one network per line (commas and spaces separate as well). The NAS checks
// and normalises the entries; here they are only split and de-duplicated.
export function parseNetworks(text: string): string[] {
	const seen = new Set<string>();
	for (const part of text.split(/[\s,;]+/)) {
		if (part) seen.add(part);
	}
	return [...seen];
}

// addNetworks appends presets (home network, Tailscale, NetBird) to the text
// without repeating entries that are already there.
export function addNetworks(text: string, more: string[]): string {
	return [...new Set([...parseNetworks(text), ...more])].join('\n');
}

export function sameNetworks(a: string[], b: string[]): boolean {
	return a.length === b.length && a.every((value, index) => value === b[index]);
}
