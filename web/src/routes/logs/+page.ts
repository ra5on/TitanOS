// Compatibility for saved links to the system journal.
import { redirect } from '@sveltejs/kit';
export function load({ url }: { url: URL }) {
	redirect(307, `/tools${url.search}#tools-logs`);
}
