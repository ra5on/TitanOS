<script lang="ts">
	import OfficeCollection from './OfficeCollection.svelte';
	import type { Source } from './api.ts';
	import { i18n } from '#lib/i18n/index.svelte.ts';
	let {
		tool,
		sources,
		onchange,
	}: {
		tool: string;
		sources: Source[];
		onchange: (args: Record<string, unknown>, valid: boolean) => void;
	} = $props();
	const de = $derived(i18n.locale === 'de');
	const collection = $derived(tool === 'office.compare' || tool === 'office.pdf-merge');
	const src = $derived(sources.length === 1 ? sources[0] : undefined);
	const ext = $derived(src?.id.split('.').at(-1)?.toLowerCase() || '');
	let paragraph = $state(1),
		slide = $state(1),
		page = $state(1),
		sheet = $state(''),
		range = $state('A1'),
		replacement = $state(''),
		name = $state(''),
		text = $state(''),
		cellType = $state('text');
	const table = $derived(['office.table', 'office.document', 'office.deck'].includes(tool));
	const write = $derived(tool !== 'office.inspect');
	const supported = $derived(['pdf', 'docx', 'xlsx', 'pptx'].includes(ext));
	const suffix = $derived(
		tool === 'office.document' ? 'docx' : tool === 'office.deck' ? 'pptx' : table ? 'xlsx' : ext,
	);
	let initialized = '';
	$effect(() => {
		const ref = src?.ref;
		const key = JSON.stringify([src?.kind, src?.id, ref]);
		if (key === initialized) return;
		initialized = key;
		paragraph = Number(ref?.paragraph) || 1;
		slide = Number(ref?.slide) || 1;
		page = Number(ref?.page) || 1;
		sheet = ref?.sheet || '';
		range = ref?.range || 'A1';
	});
	const correctName = $derived(
		!write ||
			(!!name.trim() &&
				!/[\\/]/.test(name) &&
				!name.startsWith('.') &&
				name.toLowerCase().endsWith('.' + suffix)),
	);
	$effect(() => {
		if (collection) return;
		const args: Record<string, unknown> = {};
		if (table) {
			args.text = text;
			args.name = name;
		} else {
			if (ext === 'docx' || ext === 'pptx') args.paragraph = paragraph;
			if (ext === 'pptx') args.slide = slide;
			if (ext === 'pdf') args.page = page;
			if (ext === 'xlsx') {
				args.sheet = sheet;
				args.range = range;
				args.cellType = cellType;
			}
			if (write) {
				args.name = name;
				if (ext !== 'pdf') args.replacement = replacement;
			}
		}
		const fields =
			ext === 'pdf'
				? page >= 1
				: ext === 'xlsx'
					? !!sheet.trim() && !!range.trim()
					: paragraph >= 1 && (ext !== 'pptx' || slide >= 1);
		const valid =
			correctName &&
			(table
				? src?.kind === 'folder' && !!text.trim()
				: src?.kind === 'file' && supported && fields);
		onchange(args, !!valid);
	});
</script>

{#if collection}<OfficeCollection {tool} {sources} {onchange} />{:else}
	<div class="office-tools">
		{#if table}
			<label
				>{tool === 'office.table'
					? de
						? 'Tabelleninhalt'
						: 'Table contents'
					: tool === 'office.deck'
						? de
							? 'Gliederung'
							: 'Outline'
						: de
							? 'Dokumenttext'
							: 'Document text'}<textarea
					class="field"
					bind:value={text}
					placeholder={tool === 'office.deck'
						? de
							? 'Eine Überschrift pro Folie, eine Leerzeile zwischen Folien'
							: 'One heading per slide, a blank line between slides'
						: tool === 'office.document'
							? ''
							: de
								? 'Eine Zeile pro Tabellenzeile, Tabulator zwischen Spalten'
								: 'One line per row, a tab between columns'}></textarea></label
			>
		{:else if src}
			{#if ext === 'pptx'}<label
					>{de ? 'Folie' : 'Slide'}<input
						class="field"
						type="number"
						min="1"
						bind:value={slide}
					/></label
				>{/if}
			{#if ext === 'docx' || ext === 'pptx'}<label
					>{de ? 'Absatz' : 'Paragraph'}<input
						class="field"
						type="number"
						min="1"
						bind:value={paragraph}
					/></label
				>{/if}
			{#if ext === 'pdf'}<label
					>{de ? 'Seite' : 'Page'}<input
						class="field"
						type="number"
						min="1"
						bind:value={page}
					/></label
				>{/if}
			{#if ext === 'xlsx'}<label
					>{de ? 'Tabellenblatt' : 'Sheet'}<input class="field" bind:value={sheet} /></label
				><label
					>{de ? 'Zellbereich' : 'Cell range'}<input
						class="field"
						bind:value={range}
						placeholder="A1:B3"
					/></label
				>
				{#if write}<label
						>{de ? 'Zelltyp' : 'Cell type'}<select class="field" bind:value={cellType}
							><option value="text">Text</option><option value="number"
								>{de ? 'Zahl' : 'Number'}</option
							></select
						></label
					>{/if}{/if}
			{#if write && supported && ext !== 'pdf'}<label
					>{de ? 'Neuer Inhalt' : 'Replacement'}<textarea class="field" bind:value={replacement}
					></textarea></label
				>{/if}
		{/if}
		{#if write}<label
				>{de ? 'Name der neuen Datei' : 'New filename'}<input
					class="field"
					bind:value={name}
					placeholder={'Entwurf.' + (suffix || 'docx')}
				/></label
			>{/if}
		{#if !src || (table ? src.kind !== 'folder' : !supported)}<p>
				{table
					? de
						? 'Wähle in Dateien einen Zielordner.'
						: 'Select a destination folder in Files.'
					: de
						? 'Wähle eine DOCX-, XLSX-, PDF- oder PPTX-Datei.'
						: 'Select a DOCX, XLSX, PDF or PPTX file.'}
			</p>{/if}
	</div>
{/if}

<style>
	.office-tools {
		display: grid;
		gap: 0.65rem;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 1fr));
	}
	label {
		display: grid;
		gap: 0.25rem;
		font-size: 0.9rem;
	}
	input,
	select,
	textarea {
		width: 100%;
		min-width: 0;
	}
	p {
		grid-column: 1/-1;
		margin: 0;
	}
</style>
