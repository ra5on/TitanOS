import type { DockerContainer } from './api.ts';

export type DockerView = 'list' | 'cards';
/** "own": the saved order (dragged by hand); "az": by name, stacks among containers. */
export type DockerSort = 'own' | 'az';
/** One card size for all cards: fewer (L) or more (S) side by side. */
export type DockerSize = 's' | 'm' | 'l';
export type DockerLayout = {
	version: 1;
	view: DockerView;
	sort: DockerSort;
	size: DockerSize;
	order: string[];
	details: boolean;
	primaryServices: Record<string, string>;
};
export type DockerGroup = { id: string; name: string; members: DockerContainer[] };

export function defaultDockerLayout(): DockerLayout {
	return {
		version: 1,
		view: 'list',
		sort: 'own',
		size: 'm',
		order: [],
		details: true,
		primaryServices: {},
	};
}

/** Names survive container recreation during updates. Prefixes separate apps from standalone containers. */
export function dockerGroups(containers: DockerContainer[]): DockerGroup[] {
	const groups = new Map<string, DockerGroup>();
	for (const container of containers) {
		const id = container.app ? `app:${container.app}` : `container:${container.name}`;
		let group = groups.get(id);
		if (!group) {
			group = { id, name: container.app || container.name, members: [] };
			groups.set(id, group);
		}
		group.members.push(container);
	}
	return [...groups.values()];
}

const validID = (id: unknown): id is string =>
	typeof id === 'string' && /^(app|container):[^\x00-\x1f\x7f]{1,256}$/.test(id);
export function sanitizeDockerLayout(value: unknown): DockerLayout {
	const result = defaultDockerLayout();
	if (!value || typeof value !== 'object' || Array.isArray(value)) return result;
	const input = value as Record<string, unknown>;
	// Earlier versions also had a "minimal" view that unfolded tiles on a
	// click; the list replaces it. Saved tile widths are no longer used.
	if (input.view === 'cards') result.view = 'cards';
	if (input.sort === 'az') result.sort = 'az';
	if (input.size === 's' || input.size === 'l') result.size = input.size;
	if (typeof input.details === 'boolean') result.details = input.details;
	if (Array.isArray(input.order))
		result.order = [...new Set(input.order.filter(validID))].slice(0, 2000);
	if (
		input.primaryServices &&
		typeof input.primaryServices === 'object' &&
		!Array.isArray(input.primaryServices)
	) {
		for (const [id, service] of Object.entries(input.primaryServices).slice(0, 2000)) {
			if (
				validID(id) &&
				typeof service === 'string' &&
				/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(service)
			)
				result.primaryServices[id] = service;
		}
	}
	return result;
}

export function orderedDockerGroups(groups: DockerGroup[], order: string[]): DockerGroup[] {
	const ranks = new Map(order.map((id, index) => [id, index]));
	return [...groups].sort((a, b) => (ranks.get(a.id) ?? Infinity) - (ranks.get(b.id) ?? Infinity));
}

/** The order the page shows. A–Z never changes the saved order, so switching back restores it. */
export function arrangedDockerGroups(
	groups: DockerGroup[],
	layout: Pick<DockerLayout, 'sort' | 'order'>,
	locale?: string,
): DockerGroup[] {
	if (layout.sort !== 'az') return orderedDockerGroups(groups, layout.order);
	const collator = new Intl.Collator(locale, { numeric: true, sensitivity: 'base' });
	return [...groups].sort((a, b) => collator.compare(a.name, b.name) || a.id.localeCompare(b.id));
}

/** The CPU curve of a stack: the running services added up, aligned at the newest value. */
export function stackCpuHistory(members: Pick<DockerContainer, 'cpuHistory'>[]): number[] {
	const lists = members.map((member) => member.cpuHistory ?? []).filter((list) => list.length);
	const length = Math.max(0, ...lists.map((list) => list.length));
	return Array.from({ length }, (_, index) =>
		lists.reduce((total, list) => total + (list[list.length - length + index] ?? 0), 0),
	);
}

/** Reorder the full saved sequence, retaining entries temporarily absent from Docker. */
export function moveDockerGroup(
	order: string[],
	groups: DockerGroup[],
	from: string,
	to: string,
): string[] {
	const ids = [...new Set([...order, ...groups.map((group) => group.id)])];
	if (from === to || !ids.includes(from) || !ids.includes(to)) return ids;
	const target = ids.indexOf(to);
	ids.splice(ids.indexOf(from), 1);
	ids.splice(target, 0, from);
	return ids;
}

/** An explicit missing service never silently selects a different application. */
export function stackPrimary(
	group: DockerGroup,
	services: Record<string, string>,
): DockerContainer | undefined {
	const chosen = services[group.id];
	if (chosen) return group.members.find((c) => c.service === chosen);
	return (
		group.members.find((c) => c.webui?.trim()) ??
		group.members.find((c) => c.publicUrl?.trim()) ??
		group.members.find((c) => c.proxyUrl?.trim()) ??
		group.members.find((c) => c.tailnetUrl?.trim())
	);
}
