// Checks for the "Download from link" dialog. The server checks everything
// again; these only keep the form honest while typing.

/** An http(s) link with a host and without user info. */
export function validDownloadURL(raw: string): boolean {
	if (!raw || raw.length > 8192) return false;
	try {
		const url = new URL(raw);
		return (
			(url.protocol === 'http:' || url.protocol === 'https:') &&
			!!url.hostname &&
			!url.username &&
			!url.password
		);
	} catch {
		return false;
	}
}

/** The decoded last path element of a link, or "" when it has none. */
export function downloadName(raw: string): string {
	if (!validDownloadURL(raw)) return '';
	const last = new URL(raw).pathname.split('/').pop() ?? '';
	let name = last;
	try {
		name = decodeURIComponent(last);
	} catch {
		// Keep the raw segment.
	}
	return validDownloadName(name) ? name : '';
}

/** A single file name the file manager accepts (no internal names). */
export function validDownloadName(name: string): boolean {
	return (
		name !== '' &&
		name !== '.' &&
		name !== '..' &&
		new TextEncoder().encode(name).length <= 255 &&
		!/[/\\\u0000-\u001f]/.test(name) &&
		name !== '.titanos' &&
		name !== '.recycle' &&
		!name.endsWith('.titanos-part') &&
		!name.endsWith('.titanos-part.json')
	);
}

/** An optional SHA-256 in hex. */
export function validDigest(value: string): boolean {
	return value === '' || /^[0-9a-fA-F]{64}$/.test(value);
}
