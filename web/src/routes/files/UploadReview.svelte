<script lang="ts">
	import { api, ApiError, type FileRoot, type FileEntry } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let {
		root,
		dir,
		files,
		onclose,
		onconfirm,
	}: {
		root: FileRoot;
		dir: string;
		files: { file: File; path: string }[];
		onclose: () => void;
		onconfirm: (
			files: { file: File; path: string; conflict: '' | 'rename' | 'replace' }[],
		) => Promise<void>;
	} = $props();
	type Row = {
		file: File;
		path: string;
		existing?: FileEntry;
		duplicate: boolean;
		choice: 'skip' | 'rename' | 'replace';
	};
	let rows = $state<Row[]>([]),
		checking = $state(true),
		busy = $state(false),
		error = $state(''),
		free = $state<number | null>(null),
		confirmed = $state(false);
	const accepted = $derived(
		rows.filter((row) => !(row.existing || row.duplicate) || row.choice !== 'skip'),
	);
	const bytes = $derived(accepted.reduce((total, row) => total + row.file.size, 0));
	const conflicts = $derived(rows.filter((row) => row.existing || row.duplicate));
	const replaces = $derived(conflicts.some((row) => row.choice === 'replace'));
	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	$effect(() => {
		const controller = new AbortController();
		async function inspect() {
			try {
				const places = await api.get<FileRoot[]>('/files/roots', controller.signal);
				const current = places.find((p) => p.id === root.id);
				if (!current?.writable) throw new ApiError(403, 'read_only_share', '');
				free = current.freeBytes;
				const cache = new Map<string, FileEntry[]>(),
					seen = new Set<string>(),
					result: Row[] = [];
				// Check parents first: a file in the path must never be mistaken for a missing directory.
				async function listing(parent: string): Promise<FileEntry[]> {
					if (cache.has(parent)) return cache.get(parent)!;
					if (parent) {
						const ancestor = parent.split('/').slice(0, -1).join('/');
						const name = parent.split('/').at(-1)!;
						const siblings = await listing(ancestor);
						const match = siblings.find((e) => e.name === name);
						if (!match) {
							cache.set(parent, []);
							return [];
						}
						if (!match.dir) throw new ApiError(409, 'file_exists', '');
					}
					const list = await api.get<FileEntry[]>(
						'/files/list?' + new URLSearchParams({ root: root.id, path: parent }),
						controller.signal,
					);
					cache.set(parent, list);
					return list;
				}
				for (const item of files) {
					const parent = join(dir, item.path.split('/').slice(0, -1).join('/'));
					const list = await listing(parent);
					const key = join(dir, item.path);
					const existing = list.find((e) => e.name === item.file.name);
					result.push({ ...item, existing, duplicate: seen.has(key), choice: 'rename' });
					seen.add(key);
				}
				if (!controller.signal.aborted) rows = result;
			} catch (err) {
				if (!controller.signal.aborted) error = apiErrorText(err);
			} finally {
				if (!controller.signal.aborted) checking = false;
			}
		}
		void inspect();
		return () => controller.abort();
	});
	async function start() {
		busy = true;
		error = '';
		try {
			await onconfirm(
				accepted.map((row) => ({
					file: row.file,
					path: row.path,
					conflict:
						row.existing || row.duplicate ? (row.choice === 'replace' ? 'replace' : 'rename') : '',
				})),
			);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('files.uploadReview')} {onclose} locked={busy} wide>
	<p class="mono">{root.name}/{dir}</p>
	<p>{t('files.uploadSummary', { count: accepted.length, size: formatBytes(bytes) })}</p>
	{#if free !== null}<p>{t('files.backupFree', { size: formatBytes(free) })}</p>{/if}
	{#if checking}<p role="status">{t('files.uploadChecking')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if free !== null && bytes > free}<p class="notice-error" role="alert">
			{t('files.uploadNoSpace')}
		</p>{/if}
	{#if conflicts.length}<div class="conflicts">
			{#each conflicts as row, index (index)}<div class="conflict">
					<strong class="mono">{row.path}</strong>
					<span
						>{t('files.uploadLocal')}: {formatBytes(row.file.size)} · {formatDateTime(
							new Date(row.file.lastModified),
						)}</span
					>
					<span
						>{t('files.uploadExisting')}: {row.duplicate
							? t('files.uploadDuplicate')
							: row.existing?.dir
								? t('files.folder')
								: formatBytes(row.existing?.size ?? 0)}{#if row.existing}
							· {formatDateTime(new Date(row.existing.modified))}{/if}</span
					>
					<select
						class="field"
						aria-label={row.path}
						bind:value={row.choice}
						onchange={() => (confirmed = false)}
						disabled={busy}
					>
						<option value="rename">{t('files.uploadKeepBoth')}</option><option value="skip"
							>{t('files.uploadSkip')}</option
						>
						{#if !row.existing?.dir && !row.duplicate}<option value="replace"
								>{t('files.uploadReplace')}</option
							>{/if}
					</select>
				</div>{/each}
		</div>{/if}
	{#if replaces}<label
			><input type="checkbox" bind:checked={confirmed} />{t('files.uploadReplaceConfirm')}</label
		>{/if}
	<div class="dialog-actions">
		<button class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button><button
			class="btn"
			class:btn-danger={replaces}
			class:btn-primary={!replaces}
			disabled={checking ||
				busy ||
				!!error ||
				!accepted.length ||
				free === null ||
				bytes > free ||
				(replaces && !confirmed)}
			onclick={start}>{t('files.uploadStart')}</button
		>
	</div>
</Dialog>

<style>
	.conflicts {
		max-height: 45vh;
		overflow: auto;
	}
	.conflict {
		display: grid;
		gap: 8px;
		padding: 12px 0;
		border-top: 1px solid var(--border);
	}
	p,
	strong {
		overflow-wrap: anywhere;
	}
	span {
		font-size: var(--text-md);
		color: var(--muted);
	}
</style>
