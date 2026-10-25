/** A literal image can expose its tag; expressions and digests stay whole. */
export function imageReference(image: string) {
	const raw = image.trim();
	if (!raw || raw.includes('$') || raw.includes('@'))
		return { repository: raw, tag: '', simple: false };
	const colon = raw.lastIndexOf(':');
	const tagged = colon > raw.lastIndexOf('/');
	return {
		repository: tagged ? raw.slice(0, colon) : raw,
		tag: tagged ? raw.slice(colon + 1) : 'latest',
		simple: true,
	};
}

export function validImage(image: string): boolean {
	const raw = image.trim();
	if (!raw || /\s/.test(raw)) return false;
	// Compose owns interpolation and full reference validation on the server.
	if (raw.includes('$')) return true;
	const ref = imageReference(raw);
	return !ref.simple || (!!ref.repository && /^[\w][\w.-]{0,127}$/.test(ref.tag));
}

export function withImageTag(image: string, tag: string): string {
	const ref = imageReference(image);
	return ref.simple ? `${ref.repository}:${tag.trim()}` : image;
}
