<script lang="ts">
	import { i18n, apiErrorText } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import OfficeTools from '#lib/personal-agent/OfficeTools.svelte';
	import { openPersonalAgent } from '#lib/personal-agent/open.ts';
	import type { Source } from '#lib/personal-agent/api.ts';
	import { onDestroy } from 'svelte';
	import { editorSelection, regionTitle, type EditorSelection } from './office-selection.ts';
	import { baseName, type DocRef } from './protocol.ts';
	let {
		doc,
		appId,
		dirty,
		mtime,
		version,
		requestSelection,
	}: {
		doc: DocRef;
		appId: string;
		dirty: boolean;
		mtime: string | null;
		version: string;
		requestSelection: () => Promise<EditorSelection>;
	} = $props();
	let editor = $state<EditorSelection | null>(null),
		ticket = 0;
	onDestroy(() => ticket++);
	function close() {
		ticket++;
		open = false;
		editor = null;
		valid = false;
	}
	async function show() {
		const token = ++ticket;
		open = true;
		error = '';
		editor = null;
		valid = false;
		const next = await requestSelection();
		if (ticket === token) editor = next;
	}
	const blocked = $derived(
		!editor ||
			dirty ||
			['dirty', 'unbound', 'changed'].includes(editor.status) ||
			!mtime ||
			!version,
	);

	const de = $derived(i18n.locale === 'de');
	let open = $state(false),
		working = $state(false),
		valid = $state(false),
		error = $state('');
	let selection = $state<Record<string, unknown>>({});
	const source = $derived<Source>({
		kind: 'file',
		id: doc.path,
		label: baseName(doc.path),
		ref: { root: doc.root },
	});
	function select(args: Record<string, unknown>, ok: boolean) {
		selection = args;
		valid = ok;
	}
	async function handoff() {
		if (blocked || working || (editor?.status !== 'selected' && !valid)) return;
		const token = ticket,
			initial = editor;
		working = true;
		error = '';
		try {
			const fresh = await requestSelection();
			if (token !== ticket || dirty || !initial) return;
			if (JSON.stringify(initial) !== JSON.stringify(fresh)) {
				editor = { status: 'changed' };
				valid = false;
				return;
			}
			const fields = Object.fromEntries(
				Object.entries(selection).filter(([key]) =>
					['paragraph', 'slide', 'page', 'sheet', 'range'].includes(key),
				),
			);
			const areas =
				fresh.status === 'selected'
					? fresh
					: editorSelection(appId, { status: 'selected', items: [fields] });
			if (areas.status !== 'selected' || !mtime || !version) {
				error = de
					? 'Prüfe den gewählten Bereich und die gespeicherte Datei.'
					: 'Check the selected region and saved file.';
				return;
			}
			const parts = areas.items.map((item) => ({
				...source,
				kind: 'office.region',
				label: baseName(doc.path) + ' · ' + regionTitle(item, de),
				ref: {
					root: doc.root,
					mtime,
					version,
					...Object.fromEntries(Object.entries(item).map(([k, v]) => [k, String(v)])),
				},
			}));
			await openPersonalAgent({ sources: parts, tool: 'office.inspect' });
			close();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			working = false;
		}
	}
</script>

<div class="agent-strip"><button class="btn" onclick={show}>Agent</button></div>
{#if open}
	<Dialog title={de ? 'Mit Agent bearbeiten' : 'Edit with Agent'} onclose={close}>
		<p>{baseName(doc.path)}</p>
		{#if dirty || editor?.status === 'dirty'}
			<p role="status">
				{de
					? 'Speichere die Änderungen, bevor du die Auswahl übergibst.'
					: 'Save your changes before sharing this selection.'}
			</p>
		{:else if !editor}
			<p role="status">{de ? 'Auswahl wird abgefragt …' : 'Reading editor selection …'}</p>
		{:else if editor.status === 'unbound' || !mtime || !version}
			<p role="status">
				{de
					? 'Öffne die gespeicherte NAS-Datei erneut, um ihre Auswahl zu verwenden.'
					: 'Reopen the saved NAS file to share its selection.'}
			</p>
		{:else if editor.status === 'changed'}
			<p role="status">
				{de
					? 'Die Auswahl hat sich geändert. Öffne die Auswahl erneut.'
					: 'The selection has changed. Open it again.'}
			</p>
		{:else if editor.status === 'selected'}
			<p>{de ? 'Im Editor ausgewählt' : 'Selected in the editor'}</p>
			<ul>
				{#each editor.items as item}<li>{regionTitle(item, de)}</li>{/each}
			</ul>
		{:else}
			{#if editor.status === 'unsupported'}<p role="status">
					{de
						? 'Diese Auswahl lässt sich nicht vollständig übernehmen. Wähle für diesen Auftrag einen einzelnen Bereich.'
						: 'This selection cannot be transferred in full. Choose one region for this job.'}
				</p>{/if}
			{#if editor.status === 'none'}<p>
					{de
						? 'Im Editor ist kein Bereich markiert. Wähle den Bereich manuell.'
						: 'No region is selected in the editor. Choose a region manually.'}
				</p>
			{:else if editor.status === 'legacy'}<p>
					{de ? 'Bereich manuell wählen' : 'Choose a region manually'}
				</p>{/if}
			<OfficeTools tool="office.inspect" sources={[source]} onchange={select} />
		{/if}
		{#if error}<p role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" onclick={close}>{de ? 'Schließen' : 'Close'}</button>
			<button
				class="btn btn-primary"
				disabled={blocked || working || (editor?.status !== 'selected' && !valid)}
				onclick={handoff}>{de ? 'Auswahl im Agent öffnen' : 'Open selection in Agent'}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.agent-strip {
		flex: none;
		display: flex;
		justify-content: flex-end;
		align-items: center;
		padding: 2px 8px;
		background: var(--surface);
		border-bottom: 1px solid var(--border);
	}
	.agent-strip button {
		min-height: 36px;
	}
	@media (pointer: coarse) {
		.agent-strip button {
			min-height: 44px;
		}
	}
</style>
