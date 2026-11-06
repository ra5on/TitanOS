// KI-Agent (PLAN 44, look of PLAN 48): the page on the desktop and the
// phone, the app of its own, the settings. Isolated API data only: no request reaches a NAS or an
// Ollama server.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5191';
const out = process.env.UI_OUTPUT || 'out/validation/agent';
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

const at = (h, m = 0) => new Date(Date.UTC(2026, 10, 5, h, m)).toISOString();
const models = [
	{ name: 'gpt-oss:20b', size: 13793441566, capabilities: ['completion', 'tools', 'thinking'], contextLength: 131072, levels: ['low', 'medium', 'high'], defaultLevel: 'medium' },
];
const status = { enabled: true, allowed: true, admin: true, model: 'gpt-oss:20b', think: 'medium', context: 0, volume: true, web: true, reachable: true, version: '0.40.14', models, running: [{ name: 'gpt-oss:20b', contextLength: 131072, vram: 13075291831 }] };
const projects = [
	{ id: 'p1aaaaaaaaaa0001', name: 'Home Assistant', icon: '🏠', color: '#3b82f6', instructions: 'Mein Home Assistant ist die VM HAOS. Der Container ist nur zum Testen. Vor Updates immer sichern.', location: 'Home-Assistant-Backups', created: at(1) },
	{ id: 'p1aaaaaaaaaa0002', name: 'NAS-Doku', icon: '📚', color: '#8b5cf6', created: at(1) },
	{ id: 'p1aaaaaaaaaa0003', name: 'Steuer 2026', icon: '🧾', color: '#10b981', created: at(1) },
];
const tool = (id, name, args, result, ms) => ({ id, at: at(10, 41), role: 'tool', tool: name, args, result, ok: true, durationMs: ms });
const haChat = {
	id: 'c1aaaaaaaaaa0001', title: 'Status Home Assistant', project: 'p1aaaaaaaaaa0001', created: at(10, 40), updated: at(10, 42), state: 'waiting', origin: 'web',
	pending: { id: 'pend00000000001', tool: 'vm_action', args: { name: 'HAOS', action: 'update' }, level: 'danger', summary: 'vm.update (name=HAOS)', at: at(10, 42) },
	entries: [
		{ id: 'e1', at: at(10, 40), role: 'user', content: 'Wie ist der Status von Home Assistant?' },
		{ id: 'e2', at: at(10, 40), role: 'assistant', thinking: 'Erst Docker und VMs ansehen.', calls: [{}] },
		tool('e3', 'docker_list', {}, '{}', 400),
		tool('e4', 'vm_list', {}, '{}', 300),
		tool('e5', 'ha_diagnostics', {}, '{}', 900),
		{ id: 'e6', at: at(10, 40), role: 'assistant', content: 'Ich habe Home Assistant **zweimal** gefunden:\n\n- Docker-App `homeassistant` – läuft\n- VM `HAOS` – läuft\n\nÜber welche möchtest du Bescheid wissen?', choices: ['Docker-App', 'VM HAOS', 'Beide'] },
		{ id: 'e7', at: at(10, 41), role: 'user', content: 'Beide' },
		{ id: 'e8', at: at(10, 41), role: 'assistant', thinking: 'Zustand, Version, Sicherung je Installation.', calls: [{}] },
		tool('e9', 'container_logs', { container: 'homeassistant' }, '{}', 2100),
		tool('e10', 'logs_search', { query: 'zigbee' }, '{}', 1800),
		tool('e11', 'files_search', { root: 'share/Backup', path: 'Home Assistant' }, '{}', 1200),
		{ id: 'e12', at: at(10, 42), role: 'note', note: 'memory', content: 'Home Assistant läuft als VM HAOS (Haupt) und als Test-Container.' },
		{ id: 'e13', at: at(10, 42), role: 'assistant', content: '| | Docker-App homeassistant | VM HAOS |\n|---|---|---|\n| Zustand | läuft seit 6 Tagen | läuft seit 2 Tagen |\n| Version | 2026.10.4 (aktuell) | 2026.10.1 · **Update auf 2026.10.4** |\n| Adresse | 192.168.1.20:8124 | 192.168.1.40:8123 |\n| Letzte Sicherung | heute 03:00 | vor 9 Tagen |\n| Auffällig | – | 2× „Zigbee-Stick nicht erreichbar“ |\n\nDie VM ist deine Haupt-Installation. Ich spiele das Update ein, sobald du es freigibst – vorher lege ich eine Sicherung bei den Home-Assistant-Backups ab.' },
	],
};
const docChat = {
	id: 'c1aaaaaaaaaa0002', title: 'Docker-Übersicht als Word', project: 'p1aaaaaaaaaa0002', created: at(9), updated: at(9, 5), state: 'idle', origin: 'web',
	entries: [
		{ id: 'd1', at: at(9), role: 'user', content: 'Mach mir ein Word-Dokument mit allen Docker-Apps, Version, Port und wo die Daten liegen. Leg es in die NAS-Doku.' },
		{ id: 'd2', at: at(9), role: 'assistant', calls: [{}] },
		tool('d3', 'locations_list', {}, '{}', 100),
		tool('d4', 'docker_list', {}, '{}', 600),
		{ ...tool('d5', 'file_write', { root: 'share/Documents', path: 'NAS/Docker-Übersicht 2026-11-05.docx' }, '{"ok":true}', 8800), files: [{ root: 'share/Documents', path: 'NAS/Docker-Übersicht 2026-11-05.docx' }] },
		tool('d6', 'file_read', { root: 'share/Documents', path: 'NAS/Docker-Übersicht 2026-11-05.docx' }, '{}', 1100),
		{ id: 'd7', at: at(9, 5), role: 'assistant', content: 'Fertig: **Docker-Übersicht 2026-11-05.docx** liegt in der NAS-Doku. Zwei Apps (`paperless`, `adguard`) haben ihre Daten außerhalb von `appdata` – das habe ich im Dokument markiert.' },
	],
};
const info = (c, preview) => ({ id: c.id, title: c.title, project: c.project, created: c.created, updated: c.updated, state: c.state, origin: c.origin, preview });
const chats = [
	info(haChat, 'Wartet auf Freigabe: Update der VM HAOS'),
	info(docChat, 'Fertig: Docker-Übersicht 2026-11-05.docx liegt in der NAS-Doku.'),
	{ id: 'c1aaaaaaaaaa0003', title: 'Telekom-Rechnungen einsortieren', project: 'p1aaaaaaaaaa0003', created: at(11), updated: at(11, 2), state: 'running', origin: 'web', preview: '' },
	{ id: 'c1aaaaaaaaaa0004', title: 'Warum ist die CPU so hoch?', created: at(8), updated: at(8, 1), state: 'idle', pinned: true, unread: true, preview: 'plex transkodiert zwei Streams.' },
	{ id: 'c1aaaaaaaaaa0005', title: 'Gesundheitscheck · 03.11. 08:00', created: '2026-11-03T08:00:00Z', updated: '2026-11-03T08:02:00Z', state: 'idle', origin: 'schedule', preview: 'Alles in Ordnung bis auf eine Platte mit 52 °C.' },
	{ id: 'c1aaaaaaaaaa0006', title: 'Speicher aufräumen', created: '2026-11-02T18:00:00Z', updated: '2026-11-02T18:10:00Z', state: 'idle', preview: '182 GB doppelte Videos gefunden.' },
];
const settings = {
	enabled: true, ollama: 'http://192.168.10.5:11434', model: 'gpt-oss:20b', think: 'medium', context: 0, maxSteps: 30, users: ['maria'],
	policy: { preset: 'standard', areas: { updates: 'ask' }, tools: { files_delete: 'ask' }, folders: [{ root: 'share/Documents', path: 'Privat', mode: 'deny' }, { root: 'volume/Data', path: 'appdata', mode: 'ask' }] },
	locations: [
		{ name: 'Home-Assistant-Backups', root: 'share/Backup', path: 'Home Assistant', note: 'Sicherungen von HA (VM und App)' },
		{ name: 'NAS-Doku', root: 'share/Documents', path: 'NAS', note: 'Anleitungen, Übersichten' },
		{ name: 'Steuer 2026', root: 'share/Documents', path: 'Steuer/2026', note: 'Rechnungen und Belege' },
	],
	react: true, homeassistant: { release: { domains: ['sensor'], entities: ['light.flur', 'lock.haustuer'] }, notify: ['notify.mobile_app_iphone'], entities: { 'domain:lock': 'ask' }, assist: true },
};
const tools = [
	['system_status', 'system', 'read'], ['notifications', 'system', 'read'], ['logs_search', 'system', 'read'],
	['files_list', 'files', 'read'], ['files_search', 'files', 'read'], ['file_write', 'files', 'change'], ['files_delete', 'files', 'change'],
	['docker_list', 'docker', 'read'], ['container_action', 'docker', 'change'], ['vm_list', 'vms', 'read'], ['vm_action', 'vms', 'change'],
	['backup_status', 'backups', 'read'], ['backup_action', 'backups', 'change'], ['update_action', 'updates', 'change'], ['power_action', 'updates', 'change'],
	['storage_overview', 'storage', 'read'], ['volume_action', 'storage', 'change'], ['shares_list', 'shares', 'read'], ['firewall_rule_add', 'network', 'change'],
	['accounts_list', 'accounts', 'read'], ['plugins_list', 'plugins', 'read'], ['ha_state', 'home', 'read'], ['ha_call', 'home', 'change'], ['web_search', 'web', 'read'],
].map(([name, area, kind]) => ({ name, area, kind, dangerous: kind === 'change', mode: 'allow', set: false }));

async function open(width, height, { theme = 'graphit', path = '/agent', look = null, cluster = null } = {}) {
	const made = await create('owner', theme, width);
	await made.page.setViewportSize({ width, height });
	const appearance = { theme, nav: { layout: 'side' } };
	made.data.prefs.appearance = appearance;
	await made.ctx.addInitScript(([v, l]) => {
		localStorage.setItem('titanos.appearance', JSON.stringify(v));
		if (l) localStorage.setItem('titanos.agent.look', JSON.stringify(l));
	}, [appearance, look]);
	const seen = { puts: [], posts: [] };
	await made.ctx.route('**/api/v1/**', async (route) => {
		const url = new URL(route.request().url());
		const p = url.pathname.slice(7);
		const method = route.request().method();
		const json = (value, status = 200) => route.fulfill({ status, json: value });
		if (p === '/services/state') return json({ docker: true, vms: true, lxc: true, agent: true });
		if (p === '/homeassistant/settings' && method === 'GET') return json({ domains: ['light', 'switch', 'lock', 'sensor', 'climate'], enabled: true });
		if (p === '/homeassistant/entities') return json({ entities: [
			{ id: 'light.flur', name: 'Flurlicht', area: 'Flur', state: 'off', domain: 'light' },
			{ id: 'light.wohnzimmer', name: 'Wohnzimmer', area: 'Wohnzimmer', state: 'on', domain: 'light' },
			{ id: 'lock.haustuer', name: 'Haustür', area: 'Flur', state: 'locked', domain: 'lock' },
			{ id: 'sensor.strom_tag', name: 'Strom heute', state: '4.2', domain: 'sensor' },
		] });
		if (p === '/files/list') {
			const where = url.searchParams.get('path') ?? '';
			const day = (d) => `2026-${d}T10:00:00Z`;
			if (where === '') return json([{ name: 'Post', dir: true, size: 0, modified: day('11-04') }, { name: 'NAS', dir: true, size: 0, modified: day('11-04') }]);
			if (where === 'Post') return json([
				{ name: 'Versicherung', dir: true, size: 0, modified: day('10-01') },
				{ name: 'Telekom 2026-10.pdf', dir: false, size: 186000, modified: day('11-04') },
				{ name: 'Telekom 2026-09.pdf', dir: false, size: 183000, modified: day('10-03') },
				{ name: 'Stadtwerke Abschlag.pdf', dir: false, size: 98000, modified: day('10-28') },
			]);
			return json([]);
		}
		if (p === '/files/search') return json({ entries: [{ name: 'Telekom 2026-10.pdf', path: 'Post/Telekom 2026-10.pdf', dir: false, size: 186000, modified: '2026-11-04T10:00:00Z' }], scanned: 40, truncated: false });
		if (!p.startsWith('/agent')) return route.fallback();
		if (p === '/agent/status') return json(status);
		if (p === '/agent/cluster') return json(cluster ?? { member: false, admin: false });
		if (p === '/agent/cluster/request') return json({ state: 'pending' });
		if (p.startsWith('/agent/cluster/nodes/')) return json([{ node: 'n2', name: 'nas-keller', allowed: true, asked: at(9) }]);
		if (p === '/agent/settings') {
			if (method === 'PUT') {
				seen.puts.push(route.request().postDataJSON());
				return json(route.request().postDataJSON());
			}
			return json(settings);
		}
		if (p === '/agent/tools') return json({ tools, areas: [] });
		if (p === '/agent/projects') return json({ projects });
		if (p === '/agent/chats' && method === 'GET') return json({ chats });
		if (p === '/agent/chats/' + haChat.id) return json(haChat);
		if (p === '/agent/chats/' + docChat.id) return json(docChat);
		if (p.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'event: end\ndata: {}\n\n' });
		if (p === '/agent/map') return json({ built: true, map: { built: at(3), shares: [{ name: 'Documents', root: 'share/Documents', volume: 'Data', folders: ['NAS', 'Post', 'Steuer', 'Verträge'] }, { name: 'Backup', root: 'share/Backup', volume: 'Data', folders: ['Home Assistant', 'PCs'] }], apps: [{ name: 'homeassistant', state: 'running', address: 'port 8124' }, { name: 'paperless', state: 'running', address: 'port 8000' }], vms: [{ name: 'HAOS', state: 'running', address: '192.168.1.40' }], lxc: [{ name: 'pihole', state: 'running', detail: 'debian trixie' }], backups: [{ name: 'Fotos nach USB', kind: 'b1' }], rooms: ['Flur', 'Küche', 'Wohnzimmer'] }, descriptions: { 'folder:share/Documents/Post': { text: 'Eingescannte Briefe und Rechnungen', by: 'alex', at: at(9) }, 'vm:HAOS': { text: 'Haupt-Installation von Home Assistant', by: 'agent', at: at(10) } } });
		if (p === '/agent/profile') return json({ instruction: 'Antworte kurz.' });
		if (p === '/agent/preload') return json({ loading: 'gpt-oss:20b' }, 202);
		if (p.startsWith('/agent/projects/') && p.endsWith('/files')) return json({ files: [{ root: 'share/Backup', path: 'Home Assistant/haos-2026-11-05.tar', chat: haChat.id, at: at(10, 50) }] });
		if (p === '/agent/memory') return json({ memory: [{ id: 'm1', text: 'Home Assistant läuft als VM HAOS (Haupt) und als Test-Container.', at: at(10, 42), chat: haChat.id }, { id: 'm2', text: 'Telekom-Rechnungen liegen in Documents › Post.', at: at(11, 1), untrusted: true }] });
		if (p === '/agent/skills') return json({ skills: [{ id: 'builtin-health', name: 'Health check', when: 'Gesundheitscheck', steps: '1. …', builtIn: true, updated: at(1) }, { id: 's1', name: 'Rechnungen einsortieren', when: 'Rechnungen ablegen', steps: '1. files_search …', updated: at(11) }] });
		if (p === '/agent/schedules') return json({ schedules: [{ id: 'sc1', name: 'Gesundheitscheck', task: 'Gesundheitscheck und Bericht', every: 'weekly', at: '08:00', weekday: 1, enabled: true, lastRun: '2026-11-03T08:00:00Z', lastOk: true, chat: 'c1aaaaaaaaaa0005', created: at(1) }] });
		if (method === 'POST') {
			seen.posts.push(p);
			return json({ ok: true }, 202);
		}
		return json({ error: { code: 'not_found', message: 'fixture' } }, 404);
	});
	return { ...made, seen };
}

async function noOverflow(page, what) {
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
	assert.ok(overflow <= 1, `${what}: horizontal scroll ${overflow}`);
}

// PLAN 55: the new parts on their own (UI_ONLY=55 runs only these).
async function plan55() {
	// PLAN 55: plan, summary and time limit in a chat, on the phone.
	{
		const { ctx, page } = await open(390, 844);
		const longChat = {
			...docChat,
			plan: [{ text: 'Docker-Apps lesen', done: true }, { text: 'Ablageort der Daten prüfen', done: true }, { text: 'Word-Dokument schreiben', done: false }],
			entries: [
				docChat.entries[0],
				{ id: 'z1', at: at(9, 1), role: 'note', note: 'summary', content: '- Gewünscht: Übersicht aller Docker-Apps als Word.\n- 14 Apps gelesen, 2 mit Daten außerhalb von appdata.' },
				...docChat.entries.slice(1, 5),
				{ id: 'z2', at: at(9, 4), role: 'note', note: 'verify' },
				docChat.entries[5],
				{ id: 'z3', at: at(9, 5), role: 'note', note: 'timeask' },
				{ id: 'z4', at: at(9, 5), role: 'assistant', content: 'Erledigt: Apps gelesen, Ablage geprüft. Offen: das Dokument. Nächster Schritt: schreiben.' },
				{ id: 'z5', at: at(9, 5), role: 'note', note: 'timelimit', content: '30' },
			],
		};
		await page.route('**/api/v1/agent/chats/' + docChat.id, (route) => (route.request().method() === 'GET' ? route.fulfill({ json: longChat }) : route.fallback()));
		await page.goto(base + '/agent?chat=' + docChat.id);
		const plan = page.getByRole('region', { name: 'Plan' });
		await plan.waitFor();
		assert.ok(await plan.getByText('2 von 3 erledigt').isVisible(), 'plan progress');
		assert.ok(await page.getByText('Ältere Teile des Chats zusammengefasst').isVisible(), 'summary note');
		assert.ok(await page.getByText('Nach 30 Minuten angehalten.').isVisible(), 'time limit note');
		assert.ok(await page.getByRole('button', { name: 'Weitermachen' }).isVisible(), 'continue after the time limit');
		assert.equal(await page.getByText('[TitanOS]').count(), 0, 'hints to the model stay hidden');
		await noOverflow(page, 'chat with plan');
		await plan.scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/30-phone-plan-timelimit.png` });
		await ctx.close();
	}
	// Many chats: rows of the menu keep their height (the highlight was cut).
	{
		const { ctx, page } = await open(390, 844);
		const many = Array.from({ length: 40 }, (_, i) => ({ id: 'c2aaaaaaaaaa' + String(i).padStart(4, '0'), title: 'Gesundheitscheck ' + i, created: at(10), updated: at(10, i), state: 'idle', preview: '' }));
		await page.route('**/api/v1/agent/chats', (route) => (route.request().method() === 'GET' ? route.fulfill({ json: { chats: [...chats, ...many] } }) : route.fallback()));
		await page.goto(base + '/agent?app=1');
		await page.getByRole('button', { name: /Menü|Chats/ }).first().click();
		await page.getByText('Gesundheitscheck 39').waitFor();
		const heights = await page.locator('nav.menu .item').evaluateAll((list) => list.map((e) => e.getBoundingClientRect().height));
		assert.ok(heights.length > 40 && Math.min(...heights) >= 30, `rows keep their height (${Math.min(...heights)})`);
		await ctx.close();
	}
	// Notifications on this device inside the agent app (own subscription on iOS).
	{
		const { ctx, page } = await open(390, 844);
		await page.route('**/api/v1/notifications/push', (route) => route.fulfill({ json: { publicKey: 'BCx', devices: [{ id: 'd1', name: 'iPhone', service: 'web.push.apple.com', created: at(8) }] } }));
		await page.goto(base + '/agent?app=1');
		await page.getByRole('button', { name: 'Einstellungen' }).first().click();
		await page.getByRole('button', { name: /Mitteilungen/ }).click();
		await page.getByText('Auf dem iPhone ist der Agent').waitFor();
		await page.waitForTimeout(400);
		await noOverflow(page, 'agent push');
		await page.screenshot({ path: `${out}/32-phone-agent-push.png` });
		await ctx.close();
	}
	// PLAN 55: working like an agent and code in a sandbox, in the settings.
	{
		const { ctx, page, seen } = await open(1440, 2400);
		await page.goto(base + '/settings/agent?tab=connection');
		await page.getByRole('heading', { name: 'Sprache, Bilder und Sicherheit' }).waitFor();
		assert.ok(await page.getByLabel('Werkzeuge nach Bedarf laden').isChecked(), 'tools by area on by default');
		assert.ok(await page.getByLabel('Helfer für Unteraufträge').isChecked(), 'helper on by default');
		assert.equal(await page.getByLabel('Code ausführen in einer Sandbox').isChecked(), false, 'code off by default');
		await page.getByLabel('Code ausführen in einer Sandbox').check();
		await page.getByLabel('Zeitgrenze (Sekunden)').fill('120');
		await page.getByLabel('Zeit je Auftrag (Minuten)').fill('45');
		await page.getByLabel('Code ausführen in einer Sandbox').scrollIntoViewIfNeeded();
		await page.screenshot({ path: `${out}/31-settings-code.png` });
		await page.getByRole('button', { name: 'Speichern' }).click();
		await page.getByRole('status').first().waitFor();
		const put = seen.puts.at(-1);
		assert.equal(put.code.enabled, true);
		assert.equal(put.code.timeout, 120);
		assert.equal(put.maxMinutes, 45);
		assert.notEqual(put.allTools, true);
		await ctx.close();
	}
}

try {
	await plan55();
	if (process.env.UI_ONLY === '55') {
		console.log('KI-Agent UI (PLAN 55) ok:', out);
		process.exit(0);
	}
	// Desktop in TitanOS: the agent menu beside the TitanOS menu, chat with
	// steps, answer, approval, model choice at the top (PLAN 48).
	{
		const { ctx, page, seen } = await open(1440, 1000, { path: '/agent' });
		await page.goto(base + '/agent?chat=' + haChat.id);
		await page.getByRole('heading', { name: 'Status Home Assistant' }).waitFor();
		await page.getByRole('button', { name: 'Ausführen' }).waitFor();
		assert.ok(await page.getByText('Freigabe nötig').isVisible());
		assert.ok(await page.locator('.side-nav').getByText('KI-Agent').isVisible(), 'nav entry');
		const menu = page.getByRole('navigation', { name: 'Menü des KI-Agenten' });
		assert.ok(await menu.getByText('Freigabe').first().isVisible(), 'waiting chat marked in the menu');
		await page.waitForTimeout(400);
		await page.screenshot({ path: `${out}/1-desktop-chat.png` });
		await noOverflow(page, 'desktop');
		assert.ok(seen.posts.some((x) => x === `/agent/chats/${haChat.id}/seen`), 'open chat reports it is seen');
		await page.goto(base + '/agent?chat=' + docChat.id);
		await page.getByRole('heading', { name: 'Docker-Übersicht als Word' }).waitFor();
		await page.getByText('Im Dateimanager').first().waitFor();
		await page.getByRole('button', { name: 'Modell und Denkstufe wählen' }).click();
		await page.getByRole('dialog', { name: 'Modell und Denkstufe wählen' }).getByRole('button', { name: /Hoch/ }).click();
		await page.getByRole('button', { name: 'Modell und Denkstufe wählen' }).filter({ hasText: 'Hoch' }).waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/2-desktop-document.png` });
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.locator('.sugg button', { hasText: 'Gesundheitscheck' }).click();
		assert.match(await page.locator('.composer textarea').inputValue(), /Gesundheitscheck/, 'suggestion fills the input');
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/3-desktop-new.png` });
		await menu.getByRole('button', { name: 'Darstellung' }).click();
		await page.locator('dialog[open]').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/4-desktop-look.png` });
		await page.keyboard.press('Escape');
		await menu.getByRole('button', { name: 'Wissen' }).click();
		await page.locator('dialog[open]').getByText('Telekom-Rechnungen liegen').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/5-desktop-memory.png` });
		await page.locator('dialog[open]').getByRole('tab', { name: 'NAS-Landkarte' }).click();
		await page.locator('dialog[open]').getByText('Eingescannte Briefe').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/14-desktop-map.png` });
		await ctx.close();
	}
	// Between TitanOS and the agent the page loads anew, so Safari takes the
	// right manifest for "Add to Home Screen" (PLAN 48).
	{
		const { ctx, page } = await open(1440, 900);
		await page.goto(base + '/');
		assert.equal(await page.locator('link[rel=manifest]').getAttribute('href'), '/manifest.webmanifest');
		await page.evaluate(() => ((window).__same = true));
		await page.locator('.side-nav').getByText('KI-Agent').click();
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		assert.equal(await page.evaluate(() => (window).__same ?? false), false, 'agent loaded anew');
		assert.equal(await page.locator('link[rel=manifest]').getAttribute('href'), '/agent.webmanifest');
		// Back keeps the browser's history: one step back is TitanOS again
		// (loaded anew), a second one leaves; no bouncing between the two.
		await page.goBack();
		await page.waitForURL(base + '/');
		await page.waitForFunction(() => document.querySelector('link[rel=manifest]')?.getAttribute('href') === '/manifest.webmanifest');
		await page.waitForTimeout(500);
		assert.equal(new URL(page.url()).pathname, '/', 'back stays on TitanOS');
		await page.goForward();
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.waitForFunction(() => document.querySelector('link[rel=manifest]')?.getAttribute('href') === '/agent.webmanifest');
		await page.evaluate(() => ((window).__same = true));
		await page.locator('.side-nav').getByText('Dashboard').click();
		await page.waitForURL(base + '/');
		await page.waitForFunction(() => document.querySelector('link[rel=manifest]')?.getAttribute('href') === '/manifest.webmanifest');
		assert.equal(await page.evaluate(() => (window).__same ?? false), false, 'TitanOS loaded anew');
		await ctx.close();
	}
	// The app of its own on the desktop.
	{
		const { ctx, page } = await open(1440, 900);
		await page.goto(base + '/agent?app=1&chat=' + haChat.id);
		await page.getByRole('heading', { name: 'Status Home Assistant' }).waitFor();
		assert.equal(await page.locator('.side-nav').count(), 0, 'app without TitanOS frame');
		assert.equal(await page.locator('link[rel=manifest]').getAttribute('href'), '/agent.webmanifest');
		await page.getByRole('button', { name: 'Zurück zu TitanOS' }).waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/6-app-desktop.png` });
		await ctx.close();
	}
	// Phone, installed app: start page, project sheet, menu, chat; light theme too.
	for (const [theme, suffix] of [['graphit', ''], ['hell', '-hell']]) {
		const { ctx, page } = await open(390, 844, { theme, look: { accent: 'aurora', style: 'bubbles', glass: true, glow: true, typing: true, vibrate: true, calm: false } });
		await page.goto(base + '/agent?app=1');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.waitForTimeout(400);
		await page.screenshot({ path: `${out}/7-phone-home${suffix}.png` });
		await noOverflow(page, 'phone home');
		if (!suffix) {
			await page.locator('.ptile', { hasText: 'Home Assistant' }).click();
			await page.locator('dialog[open]').waitFor();
			await page.waitForTimeout(300);
			await page.screenshot({ path: `${out}/8-phone-project.png` });
			await page.locator('dialog[open]').getByRole('tab', { name: 'Dateien' }).click();
			await page.locator('dialog[open]').getByText('haos-2026-11-05.tar').first().waitFor();
			await page.screenshot({ path: `${out}/15-phone-project-files.png` });
			await page.keyboard.press('Escape');
			await page.getByRole('button', { name: 'Menü öffnen' }).click();
			const drawer = page.locator('aside.drawer');
			await drawer.waitFor();
			await drawer.getByRole('button', { name: /Home Assistant/ }).first().click();
			await drawer.getByRole('button', { name: /Status Home Assistant/ }).click();
			await page.getByRole('button', { name: 'Ausführen' }).waitFor();
			await drawer.waitFor({ state: 'detached' }); // the menu closes after choosing a chat
			await page.waitForTimeout(400);
			await page.screenshot({ path: `${out}/9-phone-chat.png` });
			await noOverflow(page, 'phone chat');
		}
		await ctx.close();
	}
	// Phone inside TitanOS: an app of its own as well, its own menu with the
	// way back to TitanOS, no round TitanOS button (PLAN 48).
	{
		const { ctx, page } = await open(390, 844);
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.waitForTimeout(300);
		assert.equal(await page.locator('.fab').count(), 0, 'no round TitanOS menu button in the agent');
		assert.equal(await page.locator('header.bar-surface, .side-nav').count(), 0, 'no TitanOS frame on the phone');
		const size = await page.locator('.composer textarea').evaluate((e) => parseFloat(getComputedStyle(e).fontSize));
		assert.ok(size >= 16, `input font ${size}px zooms on iOS`);
		await page.screenshot({ path: `${out}/10-phone-in-titanos.png` });
		await noOverflow(page, 'phone in TitanOS');
		// Model and thinking level as a sheet from the bottom.
		await page.getByRole('button', { name: 'Modell und Denkstufe wählen' }).click();
		const sheet = page.getByRole('dialog', { name: 'Modell und Denkstufe wählen' });
		await sheet.waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/23-phone-model.png` });
		await sheet.getByRole('button', { name: /Niedrig/ }).click();
		await page.getByRole('button', { name: 'Modell und Denkstufe wählen' }).filter({ hasText: 'Niedrig' }).waitFor();
		// The menu: pinned first, unread marked, search, back to TitanOS.
		await page.getByRole('button', { name: 'Menü öffnen' }).click();
		const drawer = page.locator('aside.drawer');
		await drawer.waitFor();
		const groups = await drawer.locator('.group').allTextContents();
		assert.equal(groups[1].trim().toLowerCase(), 'angeheftet', 'pinned first: ' + groups);
		assert.ok(await drawer.locator('.item.unread', { hasText: 'Warum ist die CPU so hoch?' }).count(), 'unread chat marked');
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/16-phone-menu.png` });
		await drawer.getByRole('searchbox', { name: 'Chats durchsuchen' }).fill('CPU');
		assert.equal(await drawer.locator('.item', { hasText: 'Warum ist die CPU' }).count(), 1, 'search');
		assert.equal(await drawer.locator('.item', { hasText: 'Speicher aufräumen' }).count(), 0, 'search filters');
		await drawer.getByRole('searchbox', { name: 'Chats durchsuchen' }).fill('');
		await drawer.getByRole('button', { name: 'Zurück zu TitanOS' }).waitFor();
		await page.locator('button.scrim').click({ position: { x: 370, y: 400 } });
		await drawer.waitFor({ state: 'detached' });
		// Files from the NAS through the clip menu.
		await page.getByRole('button', { name: 'Datei anhängen' }).click();
		await page.getByRole('menuitem', { name: 'Vom NAS wählen' }).click();
		const picker = page.locator('dialog[open]');
		await picker.getByRole('button', { name: /Documents/ }).click();
		await picker.getByRole('button', { name: /Post/ }).click();
		await picker.getByText('Telekom 2026-10.pdf').click();
		await picker.getByText('Telekom 2026-09.pdf').click();
		await page.waitForTimeout(200);
		await page.screenshot({ path: `${out}/17-phone-nas-picker.png` });
		await picker.getByRole('button', { name: 'Übernehmen (2)' }).click();
		await page.locator('.composer .file', { hasText: 'Post/Telekom 2026-09.pdf' }).waitFor();
		await page.screenshot({ path: `${out}/18-phone-nas-chips.png` });
		// Too large to upload: a message instead of nothing.
		await page.locator('.composer input[type=file]').first().setInputFiles({ name: 'gross.tar', mimeType: 'application/x-tar', buffer: Buffer.alloc(21 * 1024 * 1024) });
		await page.getByRole('alert').filter({ hasText: 'gross.tar' }).waitFor();
		// A chat: everything of the desktop buttons in the ⋯ menu.
		await page.goto(base + '/agent?chat=' + docChat.id);
		await page.getByRole('heading', { name: 'Docker-Übersicht als Word' }).waitFor();
		await page.getByRole('button', { name: 'Weitere Aktionen' }).click();
		for (const item of ['Anheften', 'Umbenennen', 'In Projekt verschieben', 'Als Markdown herunterladen', 'Als PDF herunterladen', 'Löschen'])
			assert.ok(await page.getByRole('menuitem', { name: item }).count(), 'menu item ' + item);
		await page.waitForTimeout(250);
		await page.screenshot({ path: `${out}/19-phone-chat-menu.png` });
		await page.keyboard.press('Escape');
		// Back to TitanOS from the menu: a full load of the dashboard.
		await page.getByRole('button', { name: 'Menü öffnen' }).click();
		await page.locator('aside.drawer').getByRole('button', { name: 'Zurück zu TitanOS' }).click();
		await page.waitForURL(base + '/');
		await ctx.close();
	}
	// Desktop: full screen and back.
	{
		const { ctx, page } = await open(1440, 900);
		await page.goto(base + '/agent?chat=' + docChat.id);
		await page.getByRole('heading', { name: 'Docker-Übersicht als Word' }).waitFor();
		await page.locator('header.top').getByRole('button', { name: 'Vollbild' }).click();
		await page.waitForTimeout(250);
		assert.equal(await page.locator('.side-nav').count(), 0, 'full screen hides the menu');
		await page.screenshot({ path: `${out}/20-desktop-full.png` });
		await page.reload();
		await page.getByRole('heading', { name: 'Docker-Übersicht als Word' }).waitFor();
		assert.equal(await page.locator('.side-nav').count(), 0, 'full screen is remembered');
		await page.locator('header.top').getByRole('button', { name: 'Mit Menü' }).click();
		await page.locator('.side-nav').waitFor();
		await ctx.close();
	}
	// PLAN 49: planned tasks, reminders and the morning report.
	{
		const { ctx, page } = await open(1440, 1000);
		const puts = [];
		const planned = [
			{ id: 'sm1', kind: 'morning', name: 'Morgenbericht', task: 'Morgenbericht', every: 'weekdays', weekdays: [1, 2, 3, 4, 5], at: '07:00', enabled: true, report: { nas: true, backups: true, today: true }, when: 'Mo, Di, Mi, Do, Fr um 07:00', next: '2026-11-06T06:00:00Z' },
			{ id: 'sr1', kind: 'reminder', name: 'Müll', task: 'Mülltonne rausstellen', every: 'once', date: '2026-11-06', at: '19:00', enabled: true, when: 'am 06.11.2026 um 19:00', next: '2026-11-06T18:00:00Z' },
			{ id: 'sc1', name: 'Gesundheitscheck', task: 'Gesundheitscheck und Bericht', every: 'weekly', at: '08:00', weekday: 1, enabled: true, lastRun: '2026-11-03T08:00:00Z', lastOk: true, when: 'jeden Mo um 08:00', next: '2026-11-09T07:00:00Z' },
		];
		await page.route('**/api/v1/agent/schedules**', (route) => {
			const method = route.request().method();
			if (method === 'PUT') {
				puts.push({ url: route.request().url(), body: route.request().postDataJSON() });
				return route.fulfill({ json: { schedules: planned } });
			}
			if (method === 'GET') return route.fulfill({ json: { schedules: planned } });
			return route.fallback();
		});
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.getByRole('navigation', { name: 'Menü des KI-Agenten' }).getByRole('button', { name: 'Geplant' }).click();
		const dlg = page.locator('dialog[open]');
		await dlg.getByRole('heading', { name: 'Morgenbericht' }).waitFor();
		await dlg.getByText('Mülltonne').first().waitFor().catch(() => {});
		assert.ok(await dlg.getByText('Müll', { exact: true }).isVisible(), 'reminder listed');
		assert.ok(await dlg.getByText(/nächstes Mal/).first().isVisible(), 'next run shown');
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/24-planned.png` });
		await dlg.getByLabel('Home Assistant').check();
		await dlg.getByRole('button', { name: 'Speichern' }).first().click();
		await page.waitForTimeout(300);
		const morning = puts.at(-1)?.body;
		assert.equal(morning?.kind, 'morning');
		assert.equal(morning?.report?.home, true, 'morning report contents saved');
		assert.deepEqual(morning?.weekdays, [1, 2, 3, 4, 5]);
		// A reminder by hand.
		await dlg.getByRole('button', { name: '+ Auftrag oder Erinnerung' }).click();
		await dlg.getByRole('radio', { name: /Erinnerung/ }).click();
		await dlg.getByLabel('Name').fill('Arzt');
		await dlg.getByLabel('Text der Erinnerung').fill('Termin beim Zahnarzt');
		await dlg.getByLabel('Wie oft').selectOption('once');
		await dlg.getByLabel('Datum').fill('2026-11-10');
		await dlg.getByLabel('Uhrzeit').last().fill('09:00');
		await page.screenshot({ path: `${out}/25-planned-new.png` });
		await dlg.getByRole('button', { name: 'Speichern' }).last().click();
		await page.waitForTimeout(300);
		const added = puts.at(-1);
		assert.ok(added.url.endsWith('/agent/schedules/new'), added.url);
		assert.equal(added.body.kind, 'reminder');
		assert.equal(added.body.every, 'once');
		assert.equal(added.body.date, '2026-11-10');
		assert.equal(added.body.at, '09:00');
		await ctx.close();
	}
	// PLAN 49/51: microphone, a project shared by another account, read-only chat.
	{
		const { ctx, page } = await open(390, 844);
		await page.route('**/api/v1/agent/status', (route) => route.fulfill({ json: { ...status, voice: true, vision: 'gemma4:12b' } }));
		await page.route('**/api/v1/agent/projects', (route) => route.fulfill({ json: { projects: [...projects, { id: 'maria:p1aaaaaaaaaa0009', name: 'Urlaub 2027', icon: '🏔️', owner: 'maria', created: at(1), members: ['alex'] }] } }));
		const sharedChat = { id: 'c1aaaaaaaaaa0099', title: 'Fähre buchen', project: 'maria:p1aaaaaaaaaa0009', created: at(9), updated: at(9, 3), state: 'idle', author: 'maria', preview: 'Fähre ab Kiel.' };
		await page.route('**/api/v1/agent/chats', (route) => (route.request().method() === 'GET' ? route.fulfill({ json: { chats: [...chats, sharedChat] } }) : route.fallback()));
		await page.route('**/api/v1/agent/chats/c1aaaaaaaaaa0099**', (route) =>
			route.fulfill({ json: { ...sharedChat, entries: [{ id: 'x1', at: at(9), role: 'user', content: 'Wie kommen wir nach Norwegen?' }, { id: 'x2', at: at(9, 3), role: 'assistant', content: 'Fähre ab Kiel.' }], author: 'maria', readOnly: true } }),
		);
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		assert.ok(await page.getByRole('button', { name: 'Sprechen' }).isVisible(), 'microphone with speech input on');
		await page.getByRole('button', { name: 'Datei anhängen' }).click();
		assert.ok(await page.getByRole('menuitem', { name: 'Foto oder Kamera' }).isVisible(), 'photo button');
		await page.keyboard.press('Escape');
		await page.getByRole('button', { name: 'Menü öffnen' }).click();
		const drawer = page.locator('aside.drawer');
		await drawer.getByRole('button', { name: /^🏔️ Urlaub 2027|^Urlaub 2027/ }).first().click();
		await drawer.getByRole('button', { name: /Fähre buchen/ }).click();
		await page.getByText('Fähre ab Kiel.').first().waitFor();
		const input = page.locator('.composer textarea');
		assert.ok(await input.isDisabled(), 'read-only chat');
		assert.equal(await input.getAttribute('placeholder'), 'Chat von maria – nur lesen');
		assert.equal(await page.getByRole('button', { name: 'Weitere Aktionen' }).count(), 0, 'no rename/delete for a foreign chat');
		await page.screenshot({ path: `${out}/26-phone-shared-chat.png` });
		await ctx.close();
	}
	// PLAN 46: tasks as a sheet from the bottom on the phone, account button as wide as the tools.
	{
		const { ctx, page } = await open(390, 844);
		await page.route('**/api/v1/files/jobs', (route) => route.fulfill({ json: [{ id: 'j1', state: 'running', operation: 'copy', names: ['Fotos 2026'], copied: 50, bytes: 100 }] }));
		await page.goto(base + '/settings');
		await page.locator('.fab').click();
		const drawerNav = page.locator('dialog.drawer');
		await drawerNav.waitFor();
		const tools = await drawerNav.locator('.quick-access').boundingBox();
		const account = await drawerNav.locator('.account .trigger').boundingBox();
		assert.ok(Math.abs(tools.width - account.width) < 2, `account button as wide as the tools (${account.width} vs ${tools.width})`);
		assert.ok(await drawerNav.locator('.account .role').isVisible(), 'role below the name');
		await page.screenshot({ path: `${out}/27-phone-account.png` });
		await drawerNav.locator('.tasks-badge').click();
		const sheet = page.locator('dialog.sheet[open]');
		await sheet.waitFor();
		assert.equal(await drawerNav.evaluate((d) => d.open), false, 'menu closes before the sheet');
		await sheet.evaluate((d) => Promise.all(d.getAnimations({ subtree: true }).map((a) => a.finished)));
		await page.waitForTimeout(300);
		const box = await sheet.boundingBox();
		assert.ok(box.y + box.height >= 843 && box.width >= 389, `sheet at the bottom (${JSON.stringify(box)})`);
		assert.ok(await sheet.getByRole('link', { name: /Fotos 2026/ }).isVisible(), 'row opens the details');
		await page.waitForTimeout(250);
		await page.screenshot({ path: `${out}/28-phone-tasks-sheet.png` });
		await sheet.getByRole('button', { name: 'Schließen' }).click();
		await sheet.waitFor({ state: 'detached' });
		await ctx.close();
	}
	// PLAN 49: speech, pictures and snapshots in the settings.
	{
		const { ctx, page, seen } = await open(1440, 1600);
		await page.route('**/api/v1/agent/voice/check', (route) => route.fulfill({ json: { reachable: true, models: ['Systran/faster-whisper-large-v3'] } }));
		await page.goto(base + '/settings/agent');
		await page.getByRole('heading', { name: 'Sprache, Bilder und Sicherheit' }).waitFor();
		await page.getByLabel('Mikrofon-Knopf im Eingabefeld').check();
		await page.getByLabel('Adresse des Whisper-Dienstes').fill('http://192.168.10.5:8000');
		await page.getByRole('button', { name: 'Verbindung prüfen' }).click();
		await page.getByText(/Erreichbar. Modelle/).waitFor();
		await page.getByRole('heading', { name: 'Sprache, Bilder und Sicherheit' }).scrollIntoViewIfNeeded();
		await page.screenshot({ path: `${out}/29-settings-media.png` });
		await page.getByRole('button', { name: 'Speichern' }).click();
		await page.getByRole('status').first().waitFor();
		const put = seen.puts.at(-1);
		assert.equal(put.voice.enabled, true);
		assert.equal(put.voice.url, 'http://192.168.10.5:8000');
		assert.notEqual(put.noSnapshots, true, 'snapshots on by default');
		await ctx.close();
	}
	// Settings › KI-Agent.
	{
		const { ctx, page, seen } = await open(1440, 2400);
		await page.goto(base + '/settings/agent');
		await page.getByRole('heading', { name: 'Was der Agent darf' }).waitFor();
		await page.waitForTimeout(500);
		await page.screenshot({ path: `${out}/11-settings.png`, fullPage: true });
		await page.getByRole('button', { name: 'Geräte aus Home Assistant laden' }).click();
		await page.getByText('Haustür').waitFor();
		await page.getByText('Geräte für den Agenten').scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/13-settings-ha-release.png` });
		await page.getByRole('button', { name: /Dateien/ }).first().click();
		await page.getByText('files_delete').waitFor();
		await page.getByRole('button', { name: 'Speichern' }).click();
		await page.getByRole('status').waitFor();
		assert.equal(seen.puts.length, 1);
		assert.equal(seen.puts[0].ollama, 'http://192.168.10.5:11434');
		assert.deepEqual(seen.puts[0].homeassistant.release.entities, ['light.flur', 'lock.haustuer']);
		await ctx.close();
		const phone = await open(390, 844);
		await phone.page.goto(base + '/settings/agent');
		await phone.page.getByRole('heading', { name: 'Was der Agent darf' }).waitFor();
		await noOverflow(phone.page, 'settings phone');
		await phone.page.screenshot({ path: `${out}/12-settings-phone.png`, fullPage: true });
		await phone.ctx.close();
	}
	// The agent across the cluster (PLAN 47): a member chooses, the admin
	// node releases.
	{
		const member = await open(1440, 1100, { cluster: { member: true, admin: false, adminNode: 'nas-haupt', state: 'off' } });
		await member.page.goto(base + '/settings/agent');
		await member.page.getByRole('radio', { name: 'Agent von nas-haupt' }).click();
		await member.page.getByRole('button', { name: 'Freigabe bei nas-haupt anfragen' }).click();
		await member.page.getByText('Die Anfrage liegt bei nas-haupt').waitFor();
		assert.equal(await member.page.getByPlaceholder('http://192.168.1.5:11434').count(), 0, 'no own address with the admin node');
		await member.page.screenshot({ path: `${out}/21-settings-cluster-member.png` });
		await member.ctx.close();
		const admin = await open(1440, 1100, { cluster: { member: false, admin: true } });
		await admin.page.route('**/api/v1/agent/settings', (route) =>
			route.request().method() === 'GET'
				? route.fulfill({ json: { ...settings, shared: [{ node: 'n2', name: 'nas-keller', allowed: false, asked: at(9) }] } })
				: route.fallback(),
		);
		await admin.page.goto(base + '/settings/agent');
		await admin.page.getByRole('heading', { name: 'Andere Knoten im Cluster' }).waitFor();
		await admin.page.getByRole('button', { name: 'Freigeben' }).click();
		await admin.page.getByRole('button', { name: 'Entziehen' }).waitFor();
		await admin.page.getByRole('heading', { name: 'Andere Knoten im Cluster' }).scrollIntoViewIfNeeded();
		await admin.page.screenshot({ path: `${out}/22-settings-cluster-admin.png` });
		await admin.ctx.close();
	}
	const real = errors.filter((e) => !/favicon|ERR_ABORTED/.test(e));
	assert.deepEqual(real, [], 'console errors');
	console.log('KI-Agent UI ok:', out);
} finally {
	await browser.close();
}
