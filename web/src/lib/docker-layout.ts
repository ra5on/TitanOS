import type { DockerContainer } from './api.ts';

export type DockerView = 'list' | 'cards';
export type DockerLayout = { version: 1; view: DockerView; order: string[]; details: boolean };
export type DockerGroup = { id: string; name: string; members: DockerContainer[] };

export function defaultDockerLayout(): DockerLayout {
	return { version: 1, view: 'list', order: [], details: true };
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

const validID = (id: unknown): id is string => typeof id === 'string' && /^(app|container):[^\x00-\x1f\x7f]{1,256}$/.test(id);
export function sanitizeDockerLayout(value: unknown): DockerLayout {
	const result = defaultDockerLayout();
	if (!value || typeof value !== 'object' || Array.isArray(value)) return result;
	const input = value as Record<string, unknown>;
	// Earlier versions also had a "minimal" view that unfolded tiles on a
	// click; the list replaces it. Saved tile widths are no longer used.
	if (input.view === 'cards') result.view = 'cards';
	if (typeof input.details === 'boolean') result.details = input.details;
	if (Array.isArray(input.order)) result.order = [...new Set(input.order.filter(validID))].slice(0, 2000);
	return result;
}

export function orderedDockerGroups(groups: DockerGroup[], order: string[]): DockerGroup[] {
	const ranks = new Map(order.map((id, index) => [id, index]));
	return [...groups].sort((a, b) => (ranks.get(a.id) ?? Infinity) - (ranks.get(b.id) ?? Infinity));
}

/** Reorder the full saved sequence, retaining entries temporarily absent from Docker. */
export function moveDockerGroup(order: string[], groups: DockerGroup[], from: string, to: string): string[] {
	const ids = [...new Set([...order, ...groups.map(group => group.id)])];
	if (from === to || !ids.includes(from) || !ids.includes(to)) return ids;
	const target = ids.indexOf(to);
	ids.splice(ids.indexOf(from), 1);
	ids.splice(target, 0, from);
	return ids;
}
