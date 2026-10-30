/** Async API content can arrive after the browser's initial anchor scroll. */
export function revealTarget(node: HTMLElement) {
	function reveal() {
		let id: string;
		try {
			id = decodeURIComponent(location.hash.slice(1));
		} catch {
			return;
		}
		if (id && node.id === id) node.scrollIntoView({ block: 'center' });
	}
	const frame = requestAnimationFrame(reveal);
	window.addEventListener('hashchange', reveal);
	return {
		destroy() {
			cancelAnimationFrame(frame);
			window.removeEventListener('hashchange', reveal);
		},
	};
}
