/** A response like titand gives: JSON with its content type, or empty. */
export function reply(status: number, body?: unknown): Response {
	return new Response(body === undefined ? null : JSON.stringify(body), {
		status,
		headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
	});
}
