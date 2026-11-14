// PLAN 67-1: the menu bar of the desktop runs the same commands as the bars.
import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
import { t } from '#lib/i18n/index.svelte.ts';
import { isEditableText } from '#lib/text-files.ts';
import { appsFor, EDITOR, extOf, webAppKey } from '#lib/webapps/protocol.ts';
import { appForFile, webapps } from '#lib/webapps/webapps.svelte.ts';
import { hasTrash } from './commands.ts';
import type { FilesPage } from './files-page.svelte.ts';
import { join } from './files-state.svelte.ts';

/** Registers the commands of the page's menu bar; call while the page's component starts. */
export function registerFilesMenu(p: FilesPage) {
	const canWrite = () => !!p.root && p.writable && !p.trash;
	const some = () => p.selected.length > 0 && !p.trash;
	registerMenuCommands({
		// PLAN 87: pulling the list down on the phone (PhoneGestures).
		'files.refresh': { run: () => void p.load(), enabled: () => !!p.root },
		'files.mkdir': { run: () => p.command('mkdir'), enabled: canWrite },
		'files.newFile': { run: () => p.command('newFile'), enabled: canWrite },
		'files.upload': { run: () => p.command('upload'), enabled: canWrite },
		'files.uploadFolder': { run: () => p.command('uploadFolder'), enabled: canWrite },
		'files.download': { run: () => p.command('download'), enabled: some },
		'files.zip': {
			run: () => (p.downloadPaths = p.selected.map((name) => join(p.dir, name))),
			enabled: some,
		},
		'files.rename': {
			run: () => p.command('rename'),
			enabled: () => canWrite() && p.selected.length === 1,
		},
		'files.delete': { run: () => p.command('delete'), enabled: () => canWrite() && some() },
		'files.trash': { run: () => p.command('trash'), enabled: () => hasTrash(p.root) },
		'files.cut': { run: () => p.command('cut'), enabled: () => canWrite() && some() },
		'files.copy': { run: () => p.command('clipboardCopy'), enabled: some },
		'files.paste': {
			run: () => p.command('paste'),
			enabled: () => canWrite() && !!p.clipboard.content,
		},
		'files.selectAll': { run: () => p.command('selectAll'), enabled: () => !!p.root && !p.trash },
		'files.search': { run: () => p.command('deepSearch'), enabled: () => !!p.root },
		...Object.fromEntries(
			(['list', 'small', 'large'] as const).map((v) => [
				'files.view.' + v,
				{ run: () => p.command(v), checked: () => p.prefs.view === v },
			]),
		),
		...Object.fromEntries(
			(['name', 'size', 'modified'] as const).map((s) => [
				'files.sort.' + s,
				{ run: () => p.prefs.sort !== s && p.prefs.sortBy(s), checked: () => p.prefs.sort === s },
			]),
		),
		'files.hidden': { run: () => p.command('showHidden'), checked: () => p.prefs.showHidden },
		'files.twoPanes': { run: () => p.command('twoPanes'), checked: () => p.prefs.split },
		'files.back': { run: () => p.command('back'), enabled: () => !!p.steps.back.length },
		'files.forward': { run: () => p.command('forward'), enabled: () => !!p.steps.forward.length },
		'files.up': { run: () => p.command('up'), enabled: () => !!p.root && p.dir !== '' },
		'files.goto': { run: () => p.command('editPath'), enabled: () => !!p.root },
		'files.favorites': () => (p.favoriteManager = true),
		'files.preview': { run: () => one() && p.command('preview', one()), enabled: () => !!one() },
		'files.properties': {
			run: () => one() && p.command('properties', one()),
			enabled: () => !!one(),
		},
		'files.folderView': {
			run: () => p.command('folderView'),
			enabled: () => !!p.root,
			checked: () => p.prefs.folderViewSaved,
		},
		'files.undo': { run: () => p.undoPanel?.undoLast(), enabled: () => !!p.undoPanel?.canUndo() },
		// "Öffnen mit ›": the apps for the type of the one file chosen (as in its context menu).
		'files.openWith': {
			items: () => {
				const entry = one();
				if (!entry || entry.dir) return [];
				const standard = appForFile(entry.name);
				const list = appsFor(extOf(entry.name), webapps.apps).map((a) => ({
					id: webAppKey(a.id),
					text: a.name,
					checked: webAppKey(a.id) === standard,
				}));
				if (p.writableText && isEditableText(entry.name, entry.size))
					list.push({
						id: EDITOR,
						text: t('files.openWith.editor'),
						checked: standard === EDITOR,
					});
				return list;
			},
			run: (app) => {
				const entry = one();
				if (entry && app) void p.openWith(entry, app, false);
			},
		},
		// "Gehe zu › Freigaben": the places of the side bar.
		'files.shares': {
			items: () =>
				(p.roots ?? []).map((r) => ({ id: r.id, text: r.name, checked: r.id === p.rootId })),
			run: (id) => id && p.open(id, ''),
		},
		// "Gehe zu › Zuletzt": the folders visited before, newest first.
		'files.recent': {
			items: () => {
				const seen = new Set<string>();
				const out: { id: string; text: string }[] = [];
				for (const step of [...p.steps.back].reverse()) {
					const key = step.root + '\n' + step.path;
					if (seen.has(key) || (step.root === p.rootId && step.path === p.dir)) continue;
					seen.add(key);
					const name = p.roots?.find((r) => r.id === step.root)?.name ?? step.root;
					out.push({ id: key, text: step.path ? `${name} / ${step.path}` : name });
				}
				return out.slice(0, 15);
			},
			run: (key) => {
				const [id, ...path] = (key ?? '').split('\n');
				if (id) p.open(id, path.join('\n'));
			},
		},
	});
	/** The one entry chosen, if exactly one is. */
	const one = () =>
		p.selected.length === 1 && !p.trash
			? p.entries?.find((e) => e.name === p.selected[0])
			: undefined;
}
