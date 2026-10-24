import { positions, type Position, type Rect } from './positions.ts';
type Settings = {
	items: Position[];
	columns: number;
	explicit?: boolean;
	gap?: number;
	onlayout?: (rects: Record<string, Rect>, width: number) => void;
};
/** Measures content rather than imposing shared row heights, including inline VM details. */
export function positionGrid(node: HTMLElement, settings: () => Settings) {
	let frame = 0,
		disposed = false;
	const observed = new Set<Element>();
	function schedule() {
		if (!disposed && !frame) frame = requestAnimationFrame(layout);
	}
	const observer = new ResizeObserver(schedule);
	const mutations = new MutationObserver(schedule);
	function layout() {
		frame = 0;
		const config = settings(),
			width = node.clientWidth;
		const children = [...node.children].filter(
			(e): e is HTMLElement => e instanceof HTMLElement && !!e.dataset.position,
		);
		const current = new Set(children);
		for (const e of observed)
			if (e !== node && !current.has(e as HTMLElement)) {
				observer.unobserve(e);
				observed.delete(e);
			}
		const gap = config.gap ?? 12;
		const cell = (width - gap * (config.columns - 1)) / config.columns;
		const heights: Record<string, number> = {};
		for (const child of children) {
			const item = config.items.find((p) => p.id === child.dataset.position);
			const span = Math.min(config.columns, item?.span ?? 1);
			child.style.width = `${Math.max(0, cell * span + gap * (span - 1))}px`;
			child.style.position = 'absolute';
			heights[child.dataset.position!] = child.getBoundingClientRect().height;
			if (!observed.has(child)) {
				observed.add(child);
				observer.observe(child);
			}
		}
		const result = positions(config.items, config.columns, heights, config.explicit !== false);
		for (const child of children) {
			const r = result.rects[child.dataset.position!];
			if (r) {
				child.style.left = `${r.column * (cell + gap)}px`;
				child.style.top = `${r.top}px`;
			}
		}
		node.style.height = `${result.height}px`;
		config.onlayout?.(result.rects, width);
	}
	observer.observe(node);
	observed.add(node);
	mutations.observe(node, { childList: true });
	$effect(() => {
		settings();
		schedule();
	});
	schedule();
	return () => {
		disposed = true;
		cancelAnimationFrame(frame);
		observer.disconnect();
		mutations.disconnect();
	};
}
