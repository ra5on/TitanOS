import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	dockerGroups,
	sanitizeDockerLayout,
	orderedDockerGroups,
	moveDockerGroup,
	arrangedDockerGroups,
	stackCpuHistory,
} from '../src/lib/docker-layout.ts';
const container = (name, app = '', id = name) => ({ name, app, id });
test('layout identities survive recreated containers and a single-service app gaining services', () => {
	const before = dockerGroups([container('photos-web-1', 'photos', 'old'), container('dns')]);
	const after = dockerGroups([
		container('dns', '', 'new-dns'),
		container('photos-web-1', 'photos', 'new'),
		container('photos-db-1', 'photos'),
	]);
	assert.equal(before[0].id, 'app:photos');
	const ordered = orderedDockerGroups(
		after,
		before.map((g) => g.id),
	);
	assert.equal(ordered[0].id, before[0].id);
	assert.equal(ordered[0].members.length, 2);
	assert.equal(ordered[1].id, 'container:dns');
});
test('malformed preferences are bounded and cannot inject layout styles', () => {
	const saved = sanitizeDockerLayout({
		view: 'minimal',
		details: false,
		order: ['app:a', 'app:a', null, 7, 'bad'],
		widths: { 'app:a': 3, 'app:b': 8, 'app:c': '100%;display:none', 'container:d': 4 },
	});
	assert.deepEqual(saved.order, ['app:a']);
	assert.equal('widths' in saved, false);
	assert.equal(saved.details, false);
	assert.equal(saved.view, 'list');
	assert.equal(sanitizeDockerLayout({ view: 'cards' }).view, 'cards');
	assert.equal(sanitizeDockerLayout({ view: 'bogus' }).view, 'list');
	assert.equal(
		sanitizeDockerLayout({ order: Array.from({ length: 3000 }, (_, i) => 'app:' + i) }).order
			.length,
		2000,
	);
});
test('reordering retains absent apps and appends newly discovered apps', () => {
	const groups = dockerGroups([container('a', 'a'), container('c', 'c'), container('new', 'new')]);
	const order = moveDockerGroup(['app:a', 'app:absent', 'app:c'], groups, 'app:c', 'app:a');
	assert.deepEqual(order, ['app:c', 'app:a', 'app:absent', 'app:new']);
	assert.deepEqual(
		orderedDockerGroups(groups, order).map((g) => g.id),
		['app:c', 'app:a', 'app:new'],
	);
	assert.deepEqual(sanitizeDockerLayout({ order }).order, order);
});
test('A–Z sorts stacks among containers by name and keeps the own order untouched', () => {
	const groups = dockerGroups([
		container('paperless-web-1', 'paperless'),
		container('paperless-db-1', 'paperless'),
		container('Jellyfin'),
		container('adguard'),
		container('app10'),
		container('app9'),
	]);
	const layout = { sort: 'own', order: ['container:Jellyfin', 'app:paperless'] };
	assert.deepEqual(
		arrangedDockerGroups(groups, layout).map((g) => g.name),
		['Jellyfin', 'paperless', 'adguard', 'app10', 'app9'],
	);
	const az = { ...layout, sort: 'az' };
	assert.deepEqual(
		arrangedDockerGroups(groups, az, 'de').map((g) => g.name),
		['adguard', 'app9', 'app10', 'Jellyfin', 'paperless'],
	);
	assert.deepEqual(az.order, ['container:Jellyfin', 'app:paperless']);
	assert.equal(sanitizeDockerLayout({ sort: 'az' }).sort, 'az');
	assert.equal(sanitizeDockerLayout({ sort: 'zz' }).sort, 'own');
	assert.equal(sanitizeDockerLayout({}).sort, 'own');
});
test('the stack curve adds the services up at their newest values', () => {
	assert.deepEqual(
		stackCpuHistory([{ cpuHistory: [1, 2, 3] }, { cpuHistory: [10, 20] }, {}]),
		[1, 12, 23],
	);
	assert.deepEqual(stackCpuHistory([]), []);
});
