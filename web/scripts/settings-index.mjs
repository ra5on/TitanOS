// PLAN 86: the search index of the control panel, generated from the
// settings pages themselves – no hand list to forget a row in.
//
// It reads every page under src/routes/settings/<area>/ (and the components
// it uses, following `./` and `#lib/` imports) with the Svelte parser and
// takes the title of each SettingGroup, SettingRow and SettingSub, the tabs
// of a page with `?tab=`, the literal `id` of a row (its anchor) and the
// rows and groups around it. Titles are translation keys; plain text titles
// (brand names like "Tailscale") stay text.
//
//   node scripts/settings-index.mjs          writes src/lib/settings-index.gen.ts
//   node scripts/settings-index.mjs --check  fails when that file is stale
//
// A title the parser cannot resolve to a key or text (data such as a
// network's name) is listed in `unresolved`; tests/settings-index.test.mjs
// keeps that list to the known cases below, so a new setting cannot slip
// past the search unnoticed.
import { readFileSync, readdirSync, statSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'svelte/compiler';

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const src = join(web, 'src');
const lib = join(src, 'lib');
const routes = join(src, 'routes', 'settings');
export const OUTPUT = join(lib, 'settings-index.gen.ts');

const ROWS = new Set(['SettingGroup', 'SettingRow', 'SettingSub']);
// Rows inside a dialog are not on the page until the dialog opens.
const SKIP = new Set(['Dialog']);
// Settings routes that only lead elsewhere.
const REDIRECTS = new Set(['/settings/appearance/dock']);

/**
 * Values the NAS sends for a row that is a setting (Energie: the CPU and
 * disk controls of `GET /energy`); keep in step with the backend.
 */
const KNOWN_VALUES = {
	'item.key': ['governor', 'preference', 'platform', 'diskSleep'],
};
/** Template keys over data that are lists, not settings (MCP: tools by area). */
const DATA_TEMPLATES = [
	't(`mcp.area.${group.area}` as MessageKey)',
	// PLAN 91: the areas Home Assistant may ask about (a list titand names).
	't(`agent.assist.area.${area}` as MessageKey)',
];

/**
 * Whether a title the parser could not read is a translation it should have
 * found: `t('key')` or `t(`key.${x}`)` with nothing else. Names of things
 * (`peer.name`), texts with values (`t('x', { name })`) and labels of data
 * (`t(g.label)`) are data, not settings, and stay out of the index.
 */
export function missedTranslation(expr) {
	if (DATA_TEMPLATES.includes(expr.trim())) return false;
	return /^t\(\s*(['"`])[^'"`]*\1\s*(as\s+\w+\s*)?\)$/.test(expr.replace(/\s+/g, ' ').trim());
}

function read(json) {
	return JSON.parse(readFileSync(json, 'utf8'));
}
function messages(locale) {
	const all = read(join(lib, 'i18n', `${locale}.json`));
	const dir = join(lib, 'i18n', 'features');
	for (const name of readdirSync(dir).sort())
		if (name.endsWith(`.${locale}.json`)) Object.assign(all, read(join(dir, name)));
	return all;
}

/** The +page.svelte files of an area, with their paths. */
function pagesOf(area) {
	const found = [];
	const walk = (dir, path) => {
		if (!existsSync(dir)) return;
		for (const name of readdirSync(dir).sort()) {
			const full = join(dir, name);
			if (statSync(full).isDirectory()) walk(full, `${path}/${name}`);
			else if (name === '+page.svelte' && !REDIRECTS.has(path)) found.push({ file: full, path });
		}
	};
	walk(join(routes, area), `/settings/${area}`);
	return found;
}

const parsed = new Map();
function load(file) {
	if (!parsed.has(file)) {
		const source = readFileSync(file, 'utf8');
		const ast = parse(source, { modern: true });
		const script = ast.instance ? source.slice(ast.instance.start, ast.instance.end) : '';
		const module = ast.module ? source.slice(ast.module.start, ast.module.end) : '';
		const imports = new Map();
		for (const m of (script + module).matchAll(
			/import\s+(?:(\w+)|\{([^}]*)\})\s+from\s+['"]([^'"]+)['"]/g,
		)) {
			const target = m[3].startsWith('#lib/')
				? join(lib, m[3].slice(5))
				: m[3].startsWith('.')
					? resolve(dirname(file), m[3])
					: null;
			if (!target) continue;
			if (m[1]) imports.set(m[1], target);
			for (const name of (m[2] ?? '').split(',')) {
				const [orig, alias] = name
					.trim()
					.replace(/^type\s+/, '')
					.split(/\s+as\s+/);
				if (orig) imports.set(alias ?? orig, target);
			}
		}
		const snippets = new Map();
		for (const node of ast.fragment.nodes)
			if (node.type === 'SnippetBlock') snippets.set(node.expression.name, node);
		parsed.set(file, { file, source, script: script + module, ast, imports, snippets });
	}
	return parsed.get(file);
}

/** A constant array of strings in a file or a module it imports from. */
function stringArray(unit, name) {
	const pattern = new RegExp(`(?:const|let)\\s+${name}\\b[^=]*=\\s*\\[([^\\]]*)\\]`);
	let text = pattern.exec(unit.script)?.[1];
	const from = unit.imports.get(name);
	if (text === undefined && from) {
		const file = [from, `${from}.ts`].find((f) => existsSync(f) && statSync(f).isFile());
		if (file) text = pattern.exec(readFileSync(file, 'utf8'))?.[1];
	}
	if (text === undefined) return null;
	const items = [...text.matchAll(/'([^']*)'|"([^"]*)"/g)].map((m) => m[1] ?? m[2]);
	return items.length ? items : null;
}

/** The values an expression can have as strings, or null when it is data. */
function values(expr, ctx) {
	if (!expr) return null;
	switch (expr.type) {
		case 'Literal':
			return typeof expr.value === 'string' ? [expr.value] : null;
		case 'TemplateLiteral': {
			let out = [''];
			for (let i = 0; i < expr.quasis.length; i++) {
				out = out.map((s) => s + expr.quasis[i].value.cooked);
				if (i < expr.expressions.length) {
					const part =
						values(expr.expressions[i], ctx) ??
						KNOWN_VALUES[sourceOf(ctx.unit, expr.expressions[i])] ??
						null;
					if (!part) return null;
					out = out.flatMap((s) => part.map((p) => s + p));
				}
			}
			return out;
		}
		case 'Identifier': {
			const bound = ctx.bindings.get(expr.name);
			if (bound) return bound.values ?? values(bound.expr, bound.ctx);
			return null;
		}
		case 'MemberExpression': {
			if (expr.object.type !== 'Identifier' || expr.computed) return null;
			const bound = ctx.bindings.get(expr.object.name);
			if (!bound?.object) return null;
			const prop = bound.object.properties.find(
				(p) => p.key && (p.key.name ?? p.key.value) === expr.property.name,
			);
			return prop ? values(prop.value, ctx) : null;
		}
		case 'TSAsExpression':
			return values(expr.expression, ctx);
		case 'ConditionalExpression': {
			const a = values(expr.consequent, ctx);
			const b = values(expr.alternate, ctx);
			return a && b ? [...a, ...b] : null;
		}
		default:
			return null;
	}
}

/** The translation keys or texts a title expression stands for. */
function titles(expr, ctx) {
	if (!expr) return null;
	if (expr.type === 'TSAsExpression') return titles(expr.expression, ctx);
	if (expr.type === 'CallExpression' && expr.callee.type === 'Identifier') {
		if (expr.callee.name !== 't' || expr.arguments.length !== 1) return null;
		const arg = expr.arguments[0];
		// t(admin ? 'a' : 'b') is t(…) of each branch.
		if (arg.type === 'ConditionalExpression')
			return titles(
				{
					...arg,
					consequent: { ...expr, arguments: [arg.consequent] },
					alternate: { ...expr, arguments: [arg.alternate] },
				},
				ctx,
			);
		const keys = values(arg, ctx);
		return keys ? keys.map((key) => ({ key })) : null;
	}
	if (expr.type === 'ConditionalExpression') {
		const a = titles(expr.consequent, ctx);
		const b = titles(expr.alternate, ctx);
		if (!a || !b) return null;
		// admin ? … : …: one title for owners/administrators, one for users.
		const test = sourceOf(ctx.unit, expr.test).trim();
		if (/^!?\s*(admin|canAdminister|isAdmin)$/.test(test)) {
			const [forAdmin, forUser] = test.startsWith('!') ? [b, a] : [a, b];
			return [
				...forAdmin.map((x) => ({ ...x, role: 'admin' })),
				...forUser.map((x) => ({ ...x, role: 'user' })),
			];
		}
		return [...a, ...b];
	}
	if (expr.type === 'LogicalExpression') return titles(expr.right, ctx);
	if (expr.type === 'Identifier') {
		const bound = ctx.bindings.get(expr.name);
		if (bound?.expr) return titles(bound.expr, bound.ctx);
	}
	const text = values(expr, ctx);
	return text ? text.map((t) => ({ text: t })) : null;
}

function attr(node, name) {
	return node.attributes.find((a) => a.type === 'Attribute' && a.name === name);
}
/** An attribute's value as text, if it has one value. */
function attrText(node, name, ctx) {
	const a = attr(node, name);
	if (!a || a.value === true) return undefined;
	const parts = Array.isArray(a.value) ? a.value : [a.value];
	let out = [''];
	for (const part of parts) {
		const v = part.type === 'Text' ? [part.data] : values(part.expression, ctx);
		if (!v) return null;
		out = out.flatMap((s) => v.map((x) => s + x));
	}
	return out.length === 1 ? out[0] : null;
}
function attrExpr(node, name) {
	const a = attr(node, name);
	if (!a || a.value === true || Array.isArray(a.value)) return undefined;
	return a.value.expression;
}

const sourceOf = (unit, node) => unit.source.slice(node.start, node.end);

/** `tab === 'x'` (or `'x' === tab`): the tab it names. */
function tabTest(test, ctx) {
	if (!ctx.tabVar || test?.type !== 'BinaryExpression' || test.operator !== '===') return null;
	const [id, lit] =
		test.left.type === 'Identifier' ? [test.left, test.right] : [test.right, test.left];
	if (id.type !== 'Identifier' || id.name !== ctx.tabVar || lit.type !== 'Literal') return null;
	return lit.value;
}

/** A test about a value of a known list item: true or false, else null. */
function decided(test, ctx) {
	// {#if row} of a component given `row` (TailscaleSettings in its row).
	if (test?.type === 'Identifier' && typeof ctx.bindings.get(test.name)?.truth === 'boolean')
		return ctx.bindings.get(test.name).truth;
	if (
		test?.type === 'UnaryExpression' &&
		test.operator === '!' &&
		test.argument.type === 'Identifier' &&
		typeof ctx.bindings.get(test.argument.name)?.truth === 'boolean'
	)
		return !ctx.bindings.get(test.argument.name).truth;
	if (test?.type !== 'BinaryExpression' || !['===', '!=='].includes(test.operator)) return null;
	if (test.left.type !== 'MemberExpression' && test.right.type !== 'MemberExpression') return null;
	const a = values(test.left, ctx);
	const b = values(test.right, ctx);
	if (a?.length !== 1 || b?.length !== 1) return null;
	return (a[0] === b[0]) === (test.operator === '===');
}

function walk(nodes, ctx, out) {
	for (const node of nodes) visit(node, ctx, out);
}

function visit(node, ctx, out) {
	switch (node.type) {
		case 'Fragment':
			return walk(node.nodes, ctx, out);
		case 'RegularElement':
		case 'SvelteElement': {
			// <div hidden={tab !== 'glass'}>: a tab that stays in the page.
			const hidden = attrExpr(node, 'hidden');
			let next = ctx;
			if (
				ctx.tabVar &&
				hidden?.type === 'BinaryExpression' &&
				hidden.operator === '!==' &&
				hidden.left.type === 'Identifier' &&
				hidden.left.name === ctx.tabVar &&
				hidden.right.type === 'Literal'
			)
				next = { ...ctx, tab: hidden.right.value };
			return walk(node.fragment.nodes, next, out);
		}
		case 'IfBlock': {
			// {#if remote.name === 'tailscale'} inside a known list: one branch.
			const known = decided(node.test, ctx);
			if (known !== null) {
				const branch = known ? node.consequent : node.alternate;
				return branch ? walk(branch.nodes, ctx, out) : undefined;
			}
			const tab = tabTest(node.test, ctx);
			// {#if admin} shows its rows to owners and administrators only,
			// {#if !admin} to accounts with the role "user" only.
			const test = sourceOf(ctx.unit, node.test).trim();
			const role = /admin|Administer|role\s*===/.test(test);
			const admin = role && !test.startsWith('!');
			const user = role && test.startsWith('!');
			walk(
				node.consequent.nodes,
				{ ...ctx, tab: tab ?? ctx.tab, admin: ctx.admin || admin, user: ctx.user || user },
				out,
			);
			if (node.alternate) {
				// A plain {:else} after tab tests would hide which tab it is.
				if (tab && !node.alternate.nodes.some((n) => n.type === 'IfBlock' && n.elseif))
					throw new Error(`${relative(src, ctx.unit.file)}: name the last tab in {:else if}`);
				walk(node.alternate.nodes, ctx, out);
			}
			return;
		}
		case 'EachBlock': {
			// {#each [{ name: 'tailscale', title: 'Tailscale' }, …] as remote}
			// and {#each EFFECT_IDS as id}: once per known item.
			const name = node.context?.type === 'Identifier' ? node.context.name : null;
			let items = null;
			if (name && node.expression.type === 'ArrayExpression')
				items = node.expression.elements.map((e) =>
					e.type === 'ObjectExpression'
						? { object: e }
						: { values: values(e, ctx) ?? undefined, expr: e, ctx },
				);
			else if (name && node.expression.type === 'Identifier') {
				const list = stringArray(ctx.unit, node.expression.name);
				if (list) items = list.map((v) => ({ values: [v] }));
			}
			if (items)
				for (const item of items) {
					const bindings = new Map(ctx.bindings);
					bindings.set(name, item);
					walk(node.body.nodes, { ...ctx, bindings }, out);
				}
			else walk(node.body.nodes, ctx, out);
			if (node.fallback) walk(node.fallback.nodes, ctx, out);
			return;
		}
		case 'AwaitBlock':
			for (const part of [node.pending, node.then, node.catch])
				if (part) walk(part.nodes, ctx, out);
			return;
		case 'KeyBlock':
		case 'SvelteBoundary':
			return walk((node.fragment ?? node).nodes ?? [], ctx, out);
		case 'SnippetBlock':
			// Top-level snippets run where they are rendered; snippets given to
			// a component (control, description …) render inside it.
			if (ctx.unit.snippets.get(node.expression.name) === node) return;
			return walk(node.body.nodes, ctx, out);
		case 'RenderTag': {
			const call =
				node.expression.type === 'ChainExpression' ? node.expression.expression : node.expression;
			const snippet =
				call.callee?.type === 'Identifier' ? ctx.unit.snippets.get(call.callee.name) : null;
			if (!snippet || ctx.rendering.has(snippet)) return;
			const bindings = new Map(ctx.bindings);
			snippet.parameters.forEach((p, i) => {
				if (p.type === 'Identifier' && call.arguments[i])
					bindings.set(p.name, { expr: call.arguments[i], ctx });
			});
			return walk(
				snippet.body.nodes,
				{ ...ctx, bindings, rendering: new Set([...ctx.rendering, snippet]) },
				out,
			);
		}
		case 'Component':
			return component(node, ctx, out);
		default:
			return;
	}
}

function component(node, ctx, out) {
	if (SKIP.has(node.name)) return;
	if (node.name === 'Tabs') {
		tabsOf(node, ctx, out);
		return walk(node.fragment.nodes, ctx, out);
	}
	if (ROWS.has(node.name)) {
		const kind = { SettingGroup: 'group', SettingRow: 'row', SettingSub: 'sub' }[node.name];
		const href = attr(node, 'href');
		const expr = attrExpr(node, 'title');
		const found = expr ? titles(expr, ctx) : null;
		const id = attrText(node, 'id', ctx) || undefined;
		// children={parts}: a snippet of the file given as the row's parts.
		const given = snippetsIn(attrExpr(node, 'children'), ctx);
		const opens =
			kind === 'row' &&
			(given.length > 0 ||
				node.fragment.nodes.some(
					(n) => n.type !== 'SnippetBlock' && !(n.type === 'Text' && !n.data.trim()),
				));
		if (expr && !found && !href)
			out.unresolved.add(`${relative(src, ctx.unit.file)}: ${sourceOf(ctx.unit, expr)}`);
		// A row that only leads to another page is no setting of this page.
		if (!found || href) {
			for (const snippet of given)
				walk(snippet.body.nodes, { ...ctx, rendering: new Set([...ctx.rendering, snippet]) }, out);
			return walk(node.fragment.nodes, ctx, out);
		}
		// A title by role (Mein Konto: "Glocke und Töne" or "Meine
		// Benachrichtigungen"): the row and what it holds once per role.
		if (found.some((f) => f.role)) {
			for (const role of ['admin', 'user']) {
				if ((role === 'admin' && ctx.user) || (role === 'user' && ctx.admin)) continue;
				const mine = {
					...ctx,
					admin: ctx.admin || role === 'admin',
					user: ctx.user || role === 'user',
				};
				for (const title of found.filter((f) => f.role === role))
					entry(node, kind, title, id, opens, given, mine, out);
			}
			return;
		}
		found.forEach((title, i) => entry(node, kind, title, id, opens, given, ctx, out, i === 0));
		return;
	}
	const file = ctx.unit.imports.get(node.name);
	if (file && file.endsWith('.svelte') && existsSync(file) && !ctx.files.has(file)) {
		const unit = load(file);
		// Its props, for a title handed down (heading={t('…')}).
		const bindings = new Map();
		for (const a of node.attributes)
			if (a.type === 'Attribute' && a.value === true) bindings.set(a.name, { truth: true });
			else if (a.type === 'Attribute' && !Array.isArray(a.value))
				bindings.set(a.name, { expr: a.value.expression, ctx });
		walk(
			unit.ast.fragment.nodes,
			{
				...ctx,
				unit,
				bindings,
				rendering: new Set(),
				files: new Set([...ctx.files, file]),
				tabVar: ctx.tabVar && unit === ctx.root ? ctx.tabVar : null,
			},
			out,
		);
	}
	// Snippets and children given to a component render inside it.
	walk(node.fragment.nodes, ctx, out);
}

/** The entry of a group, row or part; with `inside` also what it holds. */
function entry(node, kind, title, id, opens, given, ctx, out, inside = true) {
	out.entries.push({
		page: ctx.page,
		...(title.key ? { key: title.key } : { text: title.text }),
		kind,
		tab: ctx.tab,
		within: ctx.chain.map((c) => c.title),
		// The anchor of a row inside a closed row is not on the page yet.
		id: ctx.chain.some((c) => c.opens) ? undefined : id,
		admin: ctx.admin || undefined,
		user: ctx.user || undefined,
	});
	if (!inside) return;
	const chain = [...ctx.chain, { title, opens }];
	for (const snippet of given)
		walk(
			snippet.body.nodes,
			{ ...ctx, chain, rendering: new Set([...ctx.rendering, snippet]) },
			out,
		);
	walk(node.fragment.nodes, { ...ctx, chain }, out);
}

/** The snippets of the file an attribute hands over (`children={parts}`, `a ? parts : undefined`). */
function snippetsIn(expr, ctx) {
	if (!expr) return [];
	if (expr.type === 'ConditionalExpression')
		return [...snippetsIn(expr.consequent, ctx), ...snippetsIn(expr.alternate, ctx)];
	if (expr.type !== 'Identifier') return [];
	const snippet = ctx.unit.snippets.get(expr.name);
	return snippet && !ctx.rendering.has(snippet) ? [snippet] : [];
}

/** <Tabs tabs={IDS.map((id) => ({ id, label: t(`x.tab.${id}`) }))}>: the tabs of the page. */
function tabsOf(node, ctx, out) {
	let text = sourceOf(ctx.unit, attrExpr(node, 'tabs') ?? node);
	const named = /^(\w+)$/.exec(text.trim());
	if (named) {
		const decl = new RegExp(`const\\s+${named[1]}\\b[^=]*=([\\s\\S]*?);\\n`).exec(ctx.unit.script);
		if (decl) text = decl[1];
	}
	const prefix = /t\(\s*`([\w.]+)\$\{/.exec(text)?.[1];
	const list = /(\w+)\s*\.(?:filter|map)\(/.exec(text)?.[1];
	const ids = list ? stringArray(ctx.unit, list) : null;
	if (!prefix || !ids) throw new Error(`${relative(src, ctx.unit.file)}: tabs not understood`);
	for (const id of ids)
		out.tabs.push({
			page: ctx.page,
			tab: id,
			key: `${prefix}${id}`,
			admin: ctx.admin || undefined,
		});
}

/**
 * The index of the settings pages of `areas` (settingsAreas) and of the
 * pages of other programs in `others` (otherPages of settings-find.ts).
 */
export function generate(areas, others = []) {
	const de = messages('de');
	const en = messages('en');
	const entries = [];
	const tabs = [];
	const unresolved = new Set();
	const pages = [
		...areas.flatMap((area) => pagesOf(area.id)),
		...others.map((o) => ({ file: join(src, 'routes', o.path, '+page.svelte'), path: o.path })),
	];
	for (const { file, path } of pages) {
		const unit = load(file);
		const tabVar =
			/let\s+(\w+)\s*=\s*\$state<\w+>\(\s*\w+\(\s*page\.url\.searchParams\.get\('tab'\)/.exec(
				unit.script,
			)?.[1];
		const out = { entries: [], tabs: [], unresolved };
		walk(
			unit.ast.fragment.nodes,
			{
				page: path,
				unit,
				root: unit,
				tab: undefined,
				tabVar: tabVar ?? null,
				chain: [],
				bindings: new Map(),
				rendering: new Set(),
				files: new Set([file]),
				admin: false,
				user: false,
			},
			out,
		);
		// A page with ?tab= but no <Tabs> (KI-Agent: an overview of areas):
		// its tabs are the ones its {#if tab === …} name.
		if (tabVar && !out.tabs.length) {
			const prefix = /t\(\s*`([\w.]+\.tab\.)\$\{/.exec(unit.source)?.[1];
			const ids = [...new Set(out.entries.map((e) => e.tab).filter(Boolean))];
			if (prefix)
				for (const tab of ids)
					if (`${prefix}${tab}` in de) out.tabs.push({ page: path, tab, key: `${prefix}${tab}` });
		}
		// A tab whose every row is for owners/administrators is theirs too
		// (Darstellung › Anmeldeseite).
		for (const tab of out.tabs) {
			const inTab = out.entries.filter((e) => e.tab === tab.tab);
			if (inTab.length && inTab.every((e) => e.admin)) tab.admin = true;
		}
		// PageHead's title names a page below an area (Sicherungen › Abdeckung).
		if (path.startsWith('/settings/') && path.split('/').length > 3) {
			const head = unit.ast.fragment.nodes.find(
				(n) => n.type === 'Component' && n.name === 'PageHead',
			);
			const found =
				head &&
				titles(attrExpr(head, 'title'), {
					unit,
					bindings: new Map(),
				});
			if (found?.[0]?.key)
				entries.push({ page: path, key: found[0].key, kind: 'page', within: [] });
		}
		entries.push(...out.entries);
		tabs.push(...out.tabs.filter((t, i, all) => all.findIndex((o) => o.tab === t.tab) === i));
	}
	// One entry per place: a component used in a loop is the same setting.
	const seen = new Set();
	const unique = entries.filter((e) => {
		const key = JSON.stringify([e.page, e.tab, e.within, e.key ?? e.text, e.kind]);
		if (seen.has(key)) return false;
		seen.add(key);
		return true;
	});
	for (const e of [...unique, ...tabs]) {
		const keys = [e.key, ...(e.within ?? []).map((w) => w.key)].filter(Boolean);
		for (const key of keys)
			if (!(key in de) || !(key in en)) throw new Error(`unknown translation key ${key}`);
	}
	return {
		entries: unique,
		tabs,
		unresolved: [...unresolved].sort(),
		files: [...parsed.keys()].sort(),
	};
}

function render({ entries, tabs }) {
	const title = (t) => (t.key ? { k: t.key } : { x: t.text });
	const rows = entries.map((e) => ({
		p: e.page,
		...title(e),
		kind: e.kind,
		...(e.tab ? { tab: e.tab } : {}),
		...(e.within.length ? { in: e.within.map((w) => (w.key ? w.key : `=${w.text}`)) } : {}),
		...(e.id ? { id: e.id } : {}),
		...(e.admin ? { admin: true } : {}),
		...(e.user ? { user: true } : {}),
	}));
	const tabRows = tabs.map((t) => ({
		p: t.page,
		k: t.key,
		kind: 'tab',
		tab: t.tab,
		...(t.admin ? { admin: true } : {}),
	}));
	return `// Generated by web/scripts/settings-index.mjs from the settings pages (PLAN 86).
// Do not edit: run \`npm run settings-index\` after changing a settings page.
import type { SettingsIndexEntry } from './settings-find.ts';

export const settingsIndex: SettingsIndexEntry[] = ${JSON.stringify([...tabRows, ...rows])};
`;
}

export async function output(areas, others) {
	const prettier = await import('prettier');
	const result = generate(areas, others);
	const options = (await prettier.resolveConfig(OUTPUT)) ?? {};
	const text = await prettier.format(render(result), { ...options, filepath: OUTPUT });
	return { ...result, text };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
	const { settingsAreas } = await import('../src/lib/settings-search.ts');
	const { otherPages } = await import('../src/lib/settings-find.ts');
	const result = await output(settingsAreas, otherPages);
	const unknown = result.unresolved.filter((u) =>
		missedTranslation(u.split(': ').slice(1).join(': ')),
	);
	if (process.argv.includes('--report'))
		console.log(
			JSON.stringify({ unresolved: result.unresolved, count: result.entries.length }, null, 1),
		);
	if (process.argv.includes('--check')) {
		const current = existsSync(OUTPUT) ? readFileSync(OUTPUT, 'utf8') : '';
		if (current !== result.text) {
			console.error('src/lib/settings-index.gen.ts is stale: run npm run settings-index');
			process.exit(1);
		}
	} else writeFileSync(OUTPUT, result.text);
	if (unknown.length) {
		console.error('Titles the search cannot read (add a key or list them as data):');
		for (const u of unknown) console.error('  ' + u);
		process.exit(1);
	}
}
