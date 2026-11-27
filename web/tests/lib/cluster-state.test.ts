import { afterEach, expect, test, vi } from 'vitest';
import { clusterNodes, loadClusterNodes } from '#lib/cluster.svelte.ts';

const local = { id: 'a', name: 'Büro', online: true, self: true };
const peer = { id: 'b', name: 'Archiv', online: true, self: false };
const full = { manages: true, nodes: [local, peer], version: '1.35.0' };

afterEach(() => {
	vi.unstubAllGlobals();
	Object.assign(clusterNodes, { loaded: false, failed: false, nodes: [], version: '' });
});

test('a legacy response keeps the assigned local name without granting access to peers', async () => {
	vi.stubGlobal(
		'fetch',
		vi
			.fn()
			.mockResolvedValueOnce(Response.json(full))
			.mockResolvedValueOnce(Response.json({ nodes: [local, peer], version: '0.59.0' })),
	);
	await loadClusterNodes();
	expect(clusterNodes.nodes).toEqual([local, peer]);
	await loadClusterNodes();
	expect(clusterNodes.nodes).toEqual([local]);
	expect(clusterNodes.failed).toBe(false);
});

test('a failed request or incomplete reply preserves the last good list until an authoritative refresh', async () => {
	const fetch = vi
		.fn()
		.mockResolvedValueOnce(Response.json(full))
		.mockRejectedValueOnce(new TypeError('connection lost'))
		.mockResolvedValueOnce(new Response(null, { status: 204 }))
		.mockResolvedValueOnce(Response.json({}))
		.mockResolvedValueOnce(Response.json({ nodes: [null], manages: true }))
		.mockResolvedValueOnce(Response.json({ ...full, nodes: [] }));
	vi.stubGlobal('fetch', fetch);
	await loadClusterNodes();
	for (let attempt = 0; attempt < 4; attempt++) {
		await expect(loadClusterNodes()).resolves.toBeUndefined();
		expect(clusterNodes.failed).toBe(true);
		expect(clusterNodes.loaded).toBe(true);
		expect(clusterNodes.nodes).toEqual([local, peer]);
		expect(clusterNodes.version).toBe(full.version);
	}
	await loadClusterNodes();
	expect(clusterNodes.nodes).toEqual([]);
	expect(clusterNodes.failed).toBe(false);
});
