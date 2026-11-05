// KI-Agent (PLAN 44): the page on the desktop and the phone, the app of its
// own, the settings. Isolated API data only: no request reaches a NAS or an
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
	{ id: 'c1aaaaaaaaaa0004', title: 'Warum ist die CPU so hoch?', created: at(8), updated: at(8, 1), state: 'idle', pinned: true, preview: 'plex transkodiert zwei Streams.' },
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

async function open(width, height, { theme = 'graphit', path = '/agent', look = null } = {}) {
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
		if (!p.startsWith('/agent')) return route.fallback();
		if (p === '/agent/status') return json(status);
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

try {
	// Desktop in TitanOS: list, chat with steps, answer, approval.
	{
		const { ctx, page } = await open(1440, 1000, { path: '/agent' });
		await page.goto(base + '/agent?chat=' + haChat.id);
		await page.getByRole('heading', { name: 'Status Home Assistant' }).waitFor();
		await page.getByRole('button', { name: 'Ausführen' }).waitFor();
		assert.ok(await page.getByText('Freigabe nötig').isVisible());
		assert.ok(await page.locator('.side-nav').getByText('KI-Agent').isVisible(), 'nav entry');
		await page.waitForTimeout(400);
		await page.screenshot({ path: `${out}/1-desktop-chat.png` });
		await noOverflow(page, 'desktop');
		await page.goto(base + '/agent?chat=' + docChat.id);
		await page.getByRole('heading', { name: 'Docker-Übersicht als Word' }).waitFor();
		await page.getByText('Im Dateimanager').first().waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/2-desktop-document.png` });
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: 'Was soll ich erledigen?' }).waitFor();
		await page.screenshot({ path: `${out}/3-desktop-new.png` });
		await page.locator('.chat-head').getByRole('button', { name: 'Darstellung' }).click();
		await page.locator('dialog[open]').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/4-desktop-look.png` });
		await page.keyboard.press('Escape');
		await page.locator('.chat-head').getByRole('button', { name: 'Gedächtnis, Skills, Geplant' }).click();
		await page.locator('dialog[open]').getByText('Telekom-Rechnungen liegen').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/5-desktop-memory.png` });
		await page.locator('dialog[open]').getByRole('tab', { name: 'NAS-Landkarte' }).click();
		await page.locator('dialog[open]').getByText('Eingescannte Briefe').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/14-desktop-map.png` });
		await ctx.close();
	}
	// The app of its own on the desktop.
	{
		const { ctx, page } = await open(1440, 900);
		await page.goto(base + '/agent?app=1&chat=' + haChat.id);
		await page.getByRole('heading', { name: 'Status Home Assistant' }).waitFor();
		assert.equal(await page.locator('.side-nav').count(), 0, 'app without TitanOS frame');
		assert.equal(await page.locator('link[rel=manifest]').getAttribute('href'), '/agent.webmanifest');
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/6-app-desktop.png` });
		await ctx.close();
	}
	// Phone, installed app: start page, chat, project sheet; light theme too.
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
			await page.locator('.list .row', { hasText: 'Status Home Assistant' }).click();
			await page.getByRole('button', { name: 'Ausführen' }).waitFor();
			await page.waitForTimeout(400);
			await page.screenshot({ path: `${out}/9-phone-chat.png` });
			await noOverflow(page, 'phone chat');
		}
		await ctx.close();
	}
	// Phone inside TitanOS (side navigation with the menu button).
	{
		const { ctx, page } = await open(390, 844);
		await page.goto(base + '/agent');
		await page.getByRole('heading', { name: /Guten|Hallo/ }).waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/10-phone-in-titanos.png` });
		await noOverflow(page, 'phone in TitanOS');
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
	const real = errors.filter((e) => !/favicon|ERR_ABORTED/.test(e));
	assert.deepEqual(real, [], 'console errors');
	console.log('KI-Agent UI ok:', out);
} finally {
	await browser.close();
}
