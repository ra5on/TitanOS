<script lang="ts">
	// PLAN 66, docs/WEBAPPS.md: a web app (the Craft programs) running in the
	// browser in a sandboxed frame of its own (opaque origin: no cookies, no
	// TitanOS API). host.js in the frame passes files in and saves out; this
	// view reads and writes them with the rights of the account.
	import { onDestroy, untrack } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { saveBlob } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import { hasFileDrag, takeFileDrag } from '#lib/file-drag.ts';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import { registerCloseGuard, wm } from '#lib/desktop/wm.svelte.ts';
	import FileDialog from './FileDialog.svelte';
	import { namesIn, readDoc, writeDoc } from './files.ts';
	import {
		appSrc,
		baseName,
		copyName,
		extOf,
		hostMessage,
		letterGradient,
		ownAppUrl,
		parentDir,
		saveTarget,
		type DocRef,
		type WebApp,
	} from './protocol.ts';

	type Props = {
		app: WebApp;
		/** The file of the NAS the window shows (changes when another file is opened into it). */
		doc?: DocRef;
		/** The window, for the question on closing with unsaved work. */
		winId?: string;
		/** The window now shows another file of the NAS (Save As, a copy, a dropped file). */
		ondoc?: (doc: DocRef | undefined) => void;
		/** The name of the open document, for the title bar. */
		onname?: (name: string) => void;
		/** Open another file of the NAS (Datei öffnen …, dropped from the file manager): its own window. */
		onopen?: (doc: DocRef) => void;
		/** Unsaved work appeared or went away (the phone guards Back with it). */
		onunsaved?: (unsaved: boolean) => void;
	};
	let { app, doc, winId, ondoc, onname, onopen, onunsaved }: Props = $props();

	let frame = $state<HTMLIFrameElement>();
	let phase = $state<'starting' | 'ready' | 'slow'>('starting');
	let reloads = $state(0);
	const src = $derived(
		ownAppUrl(app.url) ? appSrc(app, session.data?.user?.name ?? '') : 'about:blank',
	);
	/** Unsaved changes the app reports. */
	let appDirty = $state(false);
	/** A save that did not reach the NAS (failed, conflict not resolved): only a successful write clears it. */
	let unwritten = $state(false);
	/** The document as opened: its modification time is the condition of the next write. */
	let current = $state<{ doc: DocRef; mtime: string | null } | null>(null);
	let busy = $state(false);
	let notice = $state<
		| { kind: 'error'; text: string }
		| { kind: 'info'; text: string }
		| { kind: 'created'; name: string; bytes: ArrayBuffer }
		| null
	>(null);
	let conflict = $state<{ name: string; bytes: ArrayBuffer } | null>(null);
	let saving = $state<{ name: string; bytes: ArrayBuffer; adopt: boolean } | null>(null);
	let replace = $state<{ target: DocRef; bytes: ArrayBuffer; adopt: boolean } | null>(null);
	let picking = $state(false);
	/** The app shows a file from this PC (no file of the NAS behind it). */
	let outside = false;
	let dropping = $state<{ name: string } | null>(null);

	const unsaved = () => appDirty || unwritten;
	// Plain information goes away by itself; errors and created files stay until closed.
	$effect(() => {
		if (notice?.kind !== 'info') return;
		const timer = setTimeout(() => (notice = null), 6000);
		return () => clearTimeout(timer);
	});
	$effect(() => {
		if (!winId) return;
		registerCloseGuard(winId, unsaved);
		return () => registerCloseGuard(winId, null);
	});
	$effect(() => {
		onunsaved?.(appDirty || unwritten);
	});
	$effect(() => {
		if (!appDirty && !unwritten) return;
		const leave = (e: BeforeUnloadEvent) => e.preventDefault();
		window.addEventListener('beforeunload', leave);
		return () => window.removeEventListener('beforeunload', leave);
	});

	// No answer for a long time: say so and offer a restart or a tab.
	let slowTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		void reloads;
		clearTimeout(slowTimer);
		slowTimer = setTimeout(() => {
			if (phase === 'starting') phase = 'slow';
		}, 45000);
		return () => clearTimeout(slowTimer);
	});
	onDestroy(() => clearTimeout(slowTimer));

	function post(message: Record<string, unknown>, transfer: Transferable[] = []) {
		// The frame has no origin of its own ("null"): "*" is the only target that reaches it.
		frame?.contentWindow?.postMessage(message, '*', transfer);
	}

	/** Hands a file to the app (the bytes are transferred, not copied). */
	function give(name: string, bytes: ArrayBuffer) {
		post({ type: 'webapp-open', name, bytes }, [bytes]);
		onname?.(name);
	}

	let opened = '';
	async function openDoc(next: DocRef) {
		const key = next.root + '\n' + next.path;
		opened = key;
		busy = true;
		try {
			const { bytes, mtime } = await readDoc(next);
			if (opened !== key) return;
			current = { doc: next, mtime };
			outside = false;
			unwritten = false;
			give(baseName(next.path), bytes);
		} catch (e) {
			notice = { kind: 'error', text: t('webapps.openFailed', { error: apiErrorText(e) }) };
		} finally {
			busy = false;
		}
	}

	// The file of the window, once the app is ready (and again after a restart).
	$effect(() => {
		const d = doc;
		if (phase !== 'ready' || !d) return;
		const key = d.root + '\n' + d.path;
		if (untrack(() => opened) === key) return;
		void untrack(() => openDoc(d));
	});

	function message(event: MessageEvent) {
		if (!frame || event.source !== frame.contentWindow) return;
		const msg = hostMessage(event.data);
		if (!msg) return;
		if (msg.type === 'webapp-ready') {
			opened = '';
			phase = 'ready';
		} else if (msg.type === 'webapp-dirty') appDirty = msg.dirty;
		else if (msg.type === 'webapp-failed') {
			if (msg.text) notice = { kind: 'error', text: t('webapps.appFailed', { error: msg.text }) };
		} else if (msg.type === 'webapp-save') {
			saveAsAsked = false;
			enqueueSave(msg.name, msg.saveAs, msg.bytes);
		}
	}

	async function onSave(name: string, saveAs: boolean, bytes: ArrayBuffer) {
		const target = saveTarget(current?.doc ?? null, { name, saveAs }, app);
		if (target === 'dialog') {
			saving = { name, bytes, adopt: true };
			return;
		}
		if (target === 'created') {
			// An export or another format: never over the original; the notice offers to keep it.
			notice = { kind: 'created', name, bytes };
			post({ type: 'webapp-saved', ok: false });
			return;
		}
		// The question about a change meanwhile is open: this newer version is the one it is about.
		if (conflict) {
			conflict = { ...conflict, bytes };
			post({ type: 'webapp-saved', ok: false });
			return;
		}
		await writeOriginal(bytes);
	}

	/**
	 * Saves one after the other: each write reads the modification time the
	 * one before left (two quick Ctrl+S must not look like a change meanwhile).
	 */
	let queue: Promise<void> = Promise.resolve();
	function enqueueSave(name: string, saveAs: boolean, bytes: ArrayBuffer) {
		queue = queue.then(() => onSave(name, saveAs, bytes)).catch(() => {});
	}

	async function writeOriginal(bytes: ArrayBuffer, expect?: string) {
		if (!current) return;
		busy = true;
		unwritten = true;
		try {
			const r = await writeDoc(current.doc, bytes, expect ?? current.mtime ?? 'none');
			if (!r.ok) {
				conflict = { name: baseName(current.doc.path), bytes };
				return;
			}
			current = { doc: current.doc, mtime: r.mtime };
			unwritten = false;
			notice = null;
			post({ type: 'webapp-saved', ok: true });
		} catch (e) {
			notice = { kind: 'error', text: t('webapps.saveFailed', { error: apiErrorText(e) }) };
			post({ type: 'webapp-saved', ok: false, text: apiErrorText(e) });
		} finally {
			busy = false;
		}
	}

	/** Changed meanwhile, left as it is: the app's changes stay unsaved here. */
	function cancelConflict() {
		if (!conflict) return;
		conflict = null;
		unwritten = true;
		post({ type: 'webapp-saved', ok: false });
	}

	/** Changed meanwhile: replace it anyway (with its time now as the condition). */
	async function overwrite() {
		if (!conflict || !current) return;
		const { bytes } = conflict;
		conflict = null;
		const { currentMtime } = await import('./files.ts');
		const now = await currentMtime(current.doc);
		await writeOriginal(bytes, now ?? 'none');
	}

	/** Changed meanwhile: keep both, this version as a copy next to it. */
	async function saveCopy() {
		if (!conflict || !current) return;
		const { name, bytes } = conflict;
		conflict = null;
		const dir = parentDir(current.doc.path);
		const names = await namesIn(current.doc.root, dir);
		const copy = copyName(name, (n) => names.has(n), t('webapps.copyWord'));
		await writeNew({ root: current.doc.root, path: dir ? `${dir}/${copy}` : copy }, bytes, true);
	}

	/** A new file (Save As, an export, a copy); adopt = it becomes the window's file. */
	async function writeNew(target: DocRef, bytes: ArrayBuffer, adopt: boolean, expect = 'none') {
		busy = true;
		if (adopt) unwritten = true;
		try {
			// The buffer may be needed again for the question "replace?".
			const r = await writeDoc(target, bytes.slice(0), expect);
			if (!r.ok) {
				replace = { target, bytes, adopt };
				return;
			}
			if (adopt) {
				current = { doc: target, mtime: r.mtime };
				opened = target.root + '\n' + target.path;
				unwritten = false;
				onname?.(baseName(target.path));
				ondoc?.(target);
				post({ type: 'webapp-saved', ok: true });
			}
			notice = { kind: 'info', text: t('webapps.savedAs', { name: baseName(target.path) }) };
		} catch (e) {
			notice = { kind: 'error', text: t('webapps.saveFailed', { error: apiErrorText(e) }) };
			if (adopt) post({ type: 'webapp-saved', ok: false, text: apiErrorText(e) });
		} finally {
			busy = false;
		}
	}

	function cancelReplace() {
		if (!replace) return;
		if (replace.adopt) {
			unwritten = true;
			post({ type: 'webapp-saved', ok: false });
		}
		replace = null;
	}

	async function confirmReplace() {
		if (!replace) return;
		const { target, bytes, adopt } = replace;
		replace = null;
		const { currentMtime } = await import('./files.ts');
		await writeNew(target, bytes, adopt, (await currentMtime(target)) ?? 'none');
	}

	// --- Menu of the window -------------------------------------------------

	/** "Datei öffnen …": a file of the NAS of a type the app takes. */
	export function openPicker() {
		picking = true;
	}
	/**
	 * "Speichern unter …": host.js asks the app for Save As (its F12, a TitanOS
	 * extension of the contract); an app that does not answer gets a hint.
	 */
	let saveAsWait: ReturnType<typeof setTimeout> | undefined;
	export function saveAs() {
		frame?.focus();
		clearTimeout(saveAsWait);
		saveAsAsked = true;
		post({ type: 'webapp-command', command: 'save-as' });
		saveAsWait = setTimeout(() => {
			if (saveAsAsked) notice = { kind: 'info', text: t('webapps.saveAsHint', { app: app.name }) };
			saveAsAsked = false;
		}, 2500);
	}
	let saveAsAsked = false;
	onDestroy(() => clearTimeout(saveAsWait));
	/** PLAN 67-1: Speichern, Rückgängig … of the menu bar press the app's own keys (host bridge). */
	export function command(name: 'save' | 'undo' | 'redo' | 'find' | 'select-all') {
		frame?.focus();
		post({ type: 'webapp-command', command: name });
	}
	export function restart() {
		phase = 'starting';
		appDirty = false;
		opened = '';
		reloads++;
	}
	export function openTab() {
		window.open(app.url, '_blank', 'noopener');
	}
	export function isUnsaved() {
		return unsaved();
	}

	function picked(chosen: { root: string; path: string }) {
		picking = false;
		openOther({ root: chosen.root, path: chosen.path });
	}
	/** An empty window takes the file; one with a document leaves it to a window of its own. */
	function openOther(next: DocRef) {
		if ((!doc && !current && !outside && !appDirty) || !onopen) ondoc?.(next);
		else onopen(next);
	}

	// --- Dropping a file onto the window (draft 04) ----------------------------

	let depth = 0;
	function dragEnter(e: DragEvent) {
		const types = e.dataTransfer?.types ?? [];
		if (!types.includes('Files') && !hasFileDrag(e)) return;
		depth++;
		dropping = { name: '' };
	}
	function dragLeave() {
		depth = Math.max(0, depth - 1);
		if (!depth) dropping = null;
	}
	function dragOver(e: DragEvent) {
		if (!dropping && !wm.fileDrag) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
	}
	async function drop(e: DragEvent) {
		if (!dropping && !wm.fileDrag) return;
		e.preventDefault();
		e.stopPropagation();
		depth = 0;
		dropping = null;
		const nas = takeFileDrag(e);
		if (nas?.paths.length) {
			openOther({ root: nas.root, path: nas.paths[0] });
			return;
		}
		const file = e.dataTransfer?.files?.[0];
		if (!file || phase !== 'ready') return;
		// A file of this PC: not a file of the NAS, so saving asks where.
		current = null;
		opened = '';
		outside = true;
		ondoc?.(undefined);
		give(file.name, await file.arrayBuffer());
	}

	const icon = $derived(app.icon ?? '');
	const docName = $derived(current ? baseName(current.doc.path) : '');
	const startDir = $derived(
		current ? { root: current.doc.root, dir: parentDir(current.doc.path) } : undefined,
	);
</script>

<svelte:window onmessage={message} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="webapp"
	ondragenter={dragEnter}
	ondragleave={dragLeave}
	ondragover={dragOver}
	ondrop={drop}
>
	{#key reloads}
		<iframe
			bind:this={frame}
			{src}
			title={app.name}
			sandbox="allow-scripts allow-downloads allow-modals allow-popups"
			allow="fullscreen; clipboard-read; clipboard-write"
			referrerpolicy="no-referrer"
			data-webapp={app.id}
			class:hidden={phase !== 'ready'}
		></iframe>
	{/key}

	{#if phase !== 'ready'}
		<div class="splash" role="status" aria-live="polite">
			<DesktopIcon
				glyph=""
				gradient={letterGradient(app.id)}
				image={icon}
				name={app.name}
				size={96}
			/>
			<b class="name">{app.name}</b>
			{#if phase === 'starting'}
				<span class="bar" aria-hidden="true"><span></span></span>
				<span class="muted">{t('webapps.starting')}</span>
			{:else}
				<span class="muted">{t('webapps.slow')}</span>
				<span class="actions">
					<button type="button" class="btn" onclick={restart}>{t('webapps.restart')}</button>
					<button type="button" class="btn" onclick={openTab}>{t('desktop.window.openTab')}</button>
				</span>
			{/if}
		</div>
	{/if}

	{#if dropping || wm.fileDrag}
		<div class="drop" aria-hidden="true">
			<div class="drop-card">
				<b><Icon name="download" size={18} /> {t('webapps.dropOpen', { app: app.name })}</b>
			</div>
		</div>
	{/if}

	{#if busy}<span class="busy" role="status">{t('webapps.working')}</span>{/if}

	{#if notice}
		<div
			class="notice glass-panel"
			class:error={notice.kind === 'error'}
			role={notice.kind === 'error' ? 'alert' : 'status'}
		>
			<div class="notice-head">
				<DesktopIcon
					glyph=""
					gradient={letterGradient(app.id)}
					image={icon}
					name={app.name}
					size={22}
				/>
				<b>
					{#if notice.kind === 'created'}{t('webapps.created', { app: app.name })}
					{:else if notice.kind === 'error'}<span aria-hidden="true">! </span>{t('webapps.problem')}
					{:else}{app.name}{/if}
				</b>
				<button
					type="button"
					class="x"
					onclick={() => (notice = null)}
					aria-label={t('desktop.window.close')}><Icon name="close" size={14} /></button
				>
			</div>
			{#if notice.kind === 'created'}
				{@const made = notice}
				<p class="muted">{made.name} · {formatBytes(made.bytes.byteLength)}</p>
				<div class="notice-actions">
					<button
						type="button"
						class="btn btn-primary"
						onclick={() => {
							saving = { name: made.name, bytes: made.bytes, adopt: false };
							notice = null;
						}}>{t('webapps.keepOnNas')}</button
					>
					<button
						type="button"
						class="btn"
						onclick={() => {
							saveBlob(new Blob([made.bytes]), made.name);
							notice = null;
						}}>{t('webapps.keepOnPc')}</button
					>
				</div>
			{:else}
				<p>{notice.text}</p>
			{/if}
		</div>
	{/if}
</div>

{#if conflict}
	<Dialog title={t('webapps.conflictTitle')} onclose={cancelConflict}>
		<p>{t('webapps.conflictText', { name: conflict.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={overwrite}
				>{t('webapps.overwrite')}</button
			>
			<button type="button" class="btn" onclick={cancelConflict}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={saveCopy}
				>{t('webapps.saveCopy')}</button
			>
		</div>
	</Dialog>
{/if}

{#if replace}
	<Dialog title={t('webapps.replaceTitle')} onclose={cancelReplace}>
		<p>{t('webapps.replaceText', { name: baseName(replace.target.path) })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={confirmReplace}
				>{t('webapps.replace')}</button
			>
			<button type="button" class="btn" onclick={cancelReplace}>{t('common.cancel')}</button>
		</div>
	</Dialog>
{/if}

{#if saving}
	<FileDialog
		mode="save"
		title={t('webapps.saveTitle', { app: app.name })}
		name={saving.name}
		start={startDir}
		onpick={(chosen) => {
			// Taken before the dialog goes (its state is gone afterwards).
			const s = saving;
			saving = null;
			if (s) void writeNew({ root: chosen.root, path: chosen.path }, s.bytes, s.adopt);
		}}
		onclose={() => {
			const s = saving;
			saving = null;
			if (s?.adopt) {
				// The app counts the save as done; nothing reached the NAS.
				unwritten = true;
				post({ type: 'webapp-saved', ok: false });
			}
		}}
	/>
{/if}

{#if picking}
	<FileDialog
		mode="open"
		title={t('webapps.openTitle', { app: app.name })}
		types={app.types}
		start={startDir}
		onpick={picked}
		onclose={() => (picking = false)}
	/>
{/if}

<span class="sr-only" aria-live="polite">{docName}</span>

<style>
	.webapp {
		position: relative;
		flex: 1;
		min-width: 0;
		min-height: 0;
		display: flex;
		background: var(--bg);
	}
	iframe {
		flex: 1;
		width: 100%;
		height: 100%;
		border: 0;
		background: var(--bg);
	}
	iframe.hidden {
		visibility: hidden;
	}
	.splash {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 12px;
		padding: 16px;
		text-align: center;
		background: var(--bg);
	}
	.splash .name {
		font-size: var(--text-xl, 1.25rem);
	}
	.bar {
		width: 160px;
		height: 4px;
		border-radius: 2px;
		overflow: hidden;
		background: color-mix(in srgb, var(--text) 14%, transparent);
	}
	.bar span {
		display: block;
		width: 45%;
		height: 100%;
		border-radius: 2px;
		background: var(--accent);
		animation: slide 1.4s ease-in-out infinite;
	}
	@keyframes slide {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(230%);
		}
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		justify-content: center;
	}
	.drop {
		position: absolute;
		inset: 12px;
		display: grid;
		place-items: center;
		border: 2px dashed var(--accent);
		border-radius: calc(12px * var(--radius-scale, 1));
		background: color-mix(in srgb, var(--accent) 10%, transparent);
		z-index: 4;
	}
	.drop-card {
		padding: 16px 22px;
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--surface);
		color: var(--text);
		box-shadow: 0 10px 30px rgb(0 0 0 / 0.35);
		text-align: center;
	}
	.drop-card b {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.busy {
		position: absolute;
		top: 8px;
		left: 50%;
		transform: translateX(-50%);
		padding: 4px 12px;
		border-radius: 999px;
		background: var(--surface);
		border: 1px solid var(--border);
		font-size: var(--text-sm);
		z-index: 5;
	}
	.notice {
		position: absolute;
		right: 16px;
		bottom: 16px;
		width: min(380px, calc(100% - 32px));
		padding: 14px 16px;
		border-radius: calc(12px * var(--radius-scale, 1));
		z-index: 6;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.notice.error {
		border-left: 4px solid var(--danger, #e0393e);
	}
	.notice p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.notice-head {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.notice-head b {
		flex: 1;
		min-width: 0;
		overflow-wrap: break-word;
	}
	.notice-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.notice-actions .btn {
		flex: 1 1 140px;
	}
	.x {
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
		width: 28px;
		height: 28px;
		display: grid;
		place-items: center;
		border-radius: 6px;
		flex: none;
	}
	.x:hover {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	@media (prefers-reduced-motion: reduce) {
		.bar span {
			animation: none;
			width: 100%;
		}
	}
</style>
