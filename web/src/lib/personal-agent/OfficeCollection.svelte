<script lang="ts">
	import { i18n } from '#lib/i18n/index.svelte.ts';
	import type { Source } from './api.ts';

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
	const merge = $derived(tool === 'office.pdf-merge');
	const files = $derived(sources.filter((source) => source.kind === 'file'));
	const folders = $derived(sources.filter((source) => source.kind === 'folder'));
	const key = (source: Source) => JSON.stringify([source.ref?.root, source.id]);
	const pdfs = $derived(files.filter((source) => /\.pdf$/i.test(source.id)));
	const suffix = $derived(merge ? '.pdf' : '.xlsx');
	let name = $state('');
	let pages = $state<{ id: number; file: string; page: number }[]>([]);
	let nextId = 0;
	let selectionKey = '';
	$effect(() => {
		const current = JSON.stringify(pdfs.map(key));
		if (current === selectionKey) return;
		const previous: string[] = selectionKey ? JSON.parse(selectionKey) : [];
		selectionKey = current;
		const stillSelected = new Set(pdfs.map(key));
		pages = [
			...pages.filter((row) => stillSelected.has(row.file)),
			...pdfs
				.filter((source) => !previous.includes(key(source)))
				.map((source) => ({
					id: nextId++,
					file: key(source),
					page: Math.max(1, Number(source.ref?.page) || 1),
				})),
		];
	});
	function move(index: number, delta: number) {
		const items = [...pages];
		[items[index], items[index + delta]] = [items[index + delta], items[index]];
		pages = items;
	}
	const typesValid = $derived(
		files.every((source) =>
			merge ? /\.pdf$/i.test(source.id) : /\.(pdf|xlsx|docx|txt|md|tsv|csv)$/i.test(source.id),
		),
	);
	const ready = $derived(
		sources.length === files.length + 1 &&
			folders.length === 1 &&
			files.length >= 2 &&
			files.length <= 8 &&
			typesValid &&
			name.length > 0 &&
			name.length <= 200 &&
			!/[\\/]/.test(name) &&
			!name.startsWith('.') &&
			name.toLowerCase().endsWith(suffix) &&
			pages.length <= 20 &&
			pdfs.every((source) => pages.some((row) => row.file === key(source))) &&
			pages.every((row) => Number.isInteger(row.page) && row.page > 0 && row.page <= 10000) &&
			new Set(pages.map((row) => JSON.stringify([row.file, row.page]))).size === pages.length,
	);
	$effect(() => {
		onchange(
			{
				name,
				pages: pages.map((row) => ({
					source: files.findIndex((file) => key(file) === row.file),
					page: row.page,
				})),
			},
			ready,
		);
	});
</script>

<div class="collection">
	{#if folders.length !== 1 || files.length < 2 || files.length > 8 || !typesValid}
		<p>
			{merge
				? de
					? 'Wähle zwei bis acht PDF-Dateien und einen Zielordner.'
					: 'Select two to eight PDFs and one destination folder.'
				: de
					? 'Wähle zwei bis acht Angebote als PDF, Word-, Excel- oder Textdatei und einen Zielordner.'
					: 'Select two to eight PDF, Word, Excel or text offers and one destination folder.'}
		</p>
	{/if}
	{#if pdfs.length}
		<fieldset>
			<legend
				>{merge
					? de
						? 'Reihenfolge im neuen PDF'
						: 'Pages in the new PDF'
					: de
						? 'Ausgewertete PDF-Seiten'
						: 'PDF pages to compare'}</legend
			>
			{#each pages as row, index (row.id)}
				<div class="page-row">
					<label
						>{de ? 'Quelle' : 'Source'}
						{index + 1}
						<select class="field" bind:value={row.file}>
							{#each pdfs as file (key(file))}<option value={key(file)}
									>{file.label || file.id}</option
								>{/each}
						</select>
					</label>
					<label
						>{de ? 'Seite' : 'Page'}<input
							class="field"
							type="number"
							min="1"
							max="10000"
							bind:value={row.page}
						/></label
					>
					<div class="actions">
						{#if merge}
							<button
								type="button"
								class="pa-button"
								aria-label={de ? 'Seite nach oben' : 'Move page up'}
								disabled={index === 0}
								onclick={() => move(index, -1)}>↑</button
							>
							<button
								type="button"
								class="pa-button"
								aria-label={de ? 'Seite nach unten' : 'Move page down'}
								disabled={index === pages.length - 1}
								onclick={() => move(index, 1)}>↓</button
							>
						{/if}
						<button
							type="button"
							class="pa-button"
							aria-label={de ? 'Seite entfernen' : 'Remove page'}
							onclick={() => (pages = pages.filter((item) => item.id !== row.id))}>×</button
						>
					</div>
				</div>
			{/each}
			<button
				type="button"
				class="pa-button"
				disabled={pages.length >= 20}
				onclick={() => (pages = [...pages, { id: nextId++, file: key(pdfs[0]), page: 1 }])}
				>{de ? 'Seite hinzufügen' : 'Add page'}</button
			>
		</fieldset>
	{/if}
	<label
		>{de ? 'Neue Datei' : 'New file'}
		<input
			class="field"
			type="text"
			bind:value={name}
			placeholder={merge ? 'Auswahl.pdf' : 'Angebotsvergleich.xlsx'}
		/>
	</label>
</div>

<style>
	.collection,
	fieldset {
		display: grid;
		gap: 0.65rem;
		min-width: 0;
	}
	fieldset {
		border: 0;
		padding: 0;
		margin: 0;
	}
	legend {
		font-weight: 600;
		padding: 0;
		margin: 0 0 0.4rem;
	}
	label {
		display: grid;
		gap: 0.25rem;
		min-width: 0;
	}
	.page-row {
		display: grid;
		gap: 0.4rem;
		grid-template-columns: minmax(6rem, 2fr) minmax(4rem, 0.75fr) auto;
		align-items: end;
	}
	.actions {
		display: flex;
		gap: 0.25rem;
	}
	input,
	select {
		width: 100%;
		min-width: 0;
		box-sizing: border-box;
	}
	p {
		margin: 0;
	}
	@media (max-width: 480px) {
		.page-row {
			grid-template-columns: minmax(0, 2fr) minmax(4rem, 1fr);
		}
		.actions {
			grid-column: 1/-1;
		}
	}
</style>
