/** Keep custom names while retiring the hostname inherited from the original image. */
export function resolveTitanHostname(configured: string | undefined, current: string | undefined): string {
	const candidate = configured?.trim() || current?.trim()
	return !candidate || candidate === 'titan' ? 'titan' : candidate
}
