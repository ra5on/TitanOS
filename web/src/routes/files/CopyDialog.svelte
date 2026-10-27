<script lang="ts">
	import { api, ApiError, type CopyJob, type FileEntry, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { untrack } from 'svelte';

	type Props = {
		roots: FileRoot[];
		/** The root and the paths (inside it) that are copied or moved. */
		from: FileRoot;
		paths: string[];
		move: boolean;
		restore?: boolean;
		/** Where the folder chooser starts. */
		startDir: string;
		targetRoot?: string;
		targetDir?: string;
		onclose: () => void;
		ondone: (job: CopyJob) => void;
	};
	let {
		roots,
		from,
		paths,
		move,
		restore = false,
		startDir,
		targetRoot,
		targetDir,
		onclose,
		ondone,
	}: Props = $props();

	const targets = $derived(
		roots.filter(
			(root) => root.writable && (!restore || root.kind === 'share' || root.kind === 'volume'),
		),
	);
	// svelte-ignore state_referenced_locally
	let toRoot = $state(
		targetRoot ??
			(!restore && from.writable && targets.some((root) => root.id === from.id)
				? from.id
				: (targets[0]?.id ?? '')),
	);
	// svelte-ignore state_referenced_locally
	let dir = $state(
		targetDir ??
			(!restore && from.writable && targets.some((root) => root.id === from.id) ? startDir : ''),
	);
	let folders = $state<string[]>([]);
	let conflict = $state('rename');
	let decisions = $state<Record<string, { action: string; name: string }>>({});
	const replaces = $derived(
		conflict === 'replace' ||
			(conflict === 'individual' && Object.values(decisions).some((c) => c.action === 'replace')),
	);
	const invalidChoices = $derived(
		conflict === 'individual' &&
			Object.values(decisions).some(
				(c) =>
					c.action === 'rename' &&
					(!c.name || /[\\/\x00]/.test(c.name) || c.name === '.' || c.name === '..'),
			),
	);
	type Inspection = {
		conflicts: {
			path: string;
			sourcePath: string;
			sourceVersion: string;
			targetVersion: string;
			source: FileEntry;
			target: FileEntry;
		}[];
		truncated: boolean;
	};
	let inspection = $state<Inspection | null>(null),
		inspectionKey = $state(''),
		replaceConfirmed = $state(false);
	const targetKey = $derived(JSON.stringify([from.id, paths, toRoot, dir]));
	const compared = $derived(inspectionKey === targetKey ? inspection : null);
	$effect(() => {
		targetKey;
		replaceConfirmed = false;
		inspectionKey = '';
		inspection = null;
		inspectionError = '';
		inspectionDeferred = false;
		decisions = {};
	});
	let inspecting = $state(false),
		inspectionError = $state(''),
		inspectionDeferred = $state(false);
	let pendingKey = '',
		pendingInspection: Promise<boolean> | null = null;
	let inspectionController: AbortController | undefined;
	$effect(() => {
		const key = targetKey,
			review = conflict === 'replace' || conflict === 'individual';
		untrack(() => {
			if (pendingKey && pendingKey !== key) inspectionController?.abort();
			if (!restore && review && toRoot && !nestedTarget && !(move && sameFolder))
				void inspectTarget();
		});
	});
	$effect(() => () => inspectionController?.abort());
	let error = $state('');
	let busy = $state(false);
	let restoreName = $state(
		'Restore-' +
			new Date().toISOString().replace(/[:.]/g, '-') +
			'-' +
			crypto.getRandomValues(new Uint32Array(1))[0].toString(16).padStart(8, '0'),
	);
	let created = $state<{ root: string; path: string } | null>(null);

	const crumbs = $derived(dir ? dir.split('/') : []);
	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const restorePath = $derived(join(dir, restoreName));
	const proposedName = (name: string) => {
		const dot = name.lastIndexOf('.');
		return dot > 0 ? name.slice(0, dot) + ' (2)' + name.slice(dot) : name + ' (2)';
	};
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	let scope = $state<{ bytes: number; files: number } | null>(null),
		scopeError = $state(''),
		scopeDeferred = $state(false);
	const nestedTarget = $derived(
		!restore && from.id === toRoot && paths.some((p) => dir === p || dir.startsWith(p + '/')),
	);
	const sameFolder = $derived(
		!restore &&
			from.id === toRoot &&
			paths.every((p) => p.split('/').slice(0, -1).join('/') === dir),
	);
	const target = $derived(targets.find((r) => r.id === toRoot));
	$effect(() => {
		const source = from.id,
			selection = [...paths];
		const controller = new AbortController();
		scope = null;
		scopeError = '';
		scopeDeferred = false;
		api
			.post<{ bytes: number; files: number }>(
				'/files/inspect',
				{ root: source, paths: selection },
				controller.signal,
			)
			.then((result) => {
				if (!controller.signal.aborted) scope = result;
			})
			.catch((err) => {
				if (!controller.signal.aborted) {
					scopeDeferred = err instanceof ApiError && err.code === 'file_scan_timeout';
					scopeError = fail(err);
				}
			});
		return () => controller.abort();
	});

	async function load() {
		try {
			const entries = await api.get<FileEntry[]>(
				`/files/list?root=${encodeURIComponent(toRoot)}&path=${encodeURIComponent(dir)}`,
			);
			// A folder cannot go into itself.
			folders = entries
				.filter(
					(entry) => entry.dir && !(toRoot === from.id && paths.includes(join(dir, entry.name))),
				)
				.map((entry) => entry.name);
			error = '';
		} catch (err) {
			folders = [];
			error = fail(err);
		}
	}

	$effect(() => {
		toRoot;
		dir;
		load();
	});

	function chooseRoot(id: string) {
		toRoot = id;
		dir = '';
	}

	function inspectTarget(): Promise<boolean> {
		const key = targetKey;
		if (inspectionKey === key) return Promise.resolve(true);
		if (pendingKey === key && pendingInspection) return pendingInspection;
		inspectionController?.abort();
		const controller = new AbortController();
		inspectionController = controller;
		pendingKey = key;
		inspecting = true;
		inspectionError = '';
		inspectionDeferred = false;
		const request = { root: from.id, paths: [...paths], toRoot, toDir: dir };
		pendingInspection = api
			.post<Inspection>('/files/inspect', request, controller.signal)
			.then((result) => {
				if (controller.signal.aborted || key !== targetKey) return false;
				inspection = result;
				inspectionKey = key;
				replaceConfirmed = false;
				decisions = Object.fromEntries(
					result.conflicts.map((c) => [
						c.sourcePath ?? c.source.name,
						{ action: 'skip', name: proposedName(c.source.name) },
					]),
				);
				return true;
			})
			.catch((err) => {
				if (!controller.signal.aborted && key === targetKey) {
					inspectionDeferred = err instanceof ApiError && err.code === 'file_scan_timeout';
					inspectionError =
						inspectionDeferred && !replaces && conflict !== 'individual' ? '' : fail(err);
				}
				return false;
			})
			.finally(() => {
				if (inspectionController === controller) {
					inspecting = false;
					pendingKey = '';
					pendingInspection = null;
				}
			});
		return pendingInspection;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (busy || (!scope && !scopeDeferred) || nestedTarget || (move && sameFolder)) return;
		if (
			restore &&
			(!restoreName ||
				restoreName === '.' ||
				restoreName === '..' ||
				/[/\\\x00-\x1f]/.test(restoreName))
		) {
			error = t('files.restoreInvalid');
			return;
		}
		busy = true;
		try {
			let destination = dir;
			if (!restore) {
				if (!targets.some((root) => root.id === toRoot)) {
					busy = false;
					error = t('files.targetUnavailable');
					return;
				}
				if (inspectionKey !== targetKey) {
					if (
						!(await inspectTarget()) &&
						!(inspectionDeferred && !replaces && conflict !== 'individual')
					) {
						busy = false;
						return;
					}
					if (compared?.conflicts.length) {
						busy = false;
						return;
					}
				}
				if (invalidChoices || (conflict === 'individual' && compared?.truncated)) {
					busy = false;
					return;
				}
				if (replaces && (!replaceConfirmed || compared?.truncated)) {
					busy = false;
					return;
				}
			}
			if (restore) {
				destination = restorePath;
				if (created?.root !== toRoot || created?.path !== destination) {
					await api.post('/files/mkdir', { root: toRoot, path: destination });
					created = { root: toRoot, path: destination };
				}
			}
			ondone(
				await api.post<CopyJob>('/files/copy', {
					root: from.id,
					paths,
					toRoot,
					toDir: destination,
					move: restore ? false : move,
					conflict: restore ? 'rename' : conflict === 'individual' ? 'skip' : conflict,
					choices:
						!restore && conflict === 'individual'
							? Object.fromEntries(
									(compared?.conflicts ?? []).map((c) => {
										const key = c.sourcePath ?? c.source.name,
											choice = decisions[key];
										return [
											key,
											{
												action: choice.action,
												name: choice.action === 'rename' ? choice.name : '',
												sourceVersion: c.sourceVersion,
												targetVersion: choice.action === 'rename' ? 'absent' : c.targetVersion,
											},
										];
									}),
								)
							: undefined,
				}),
			);
		} catch (err) {
			error = fail(err);
			busy = false;
		}
	}
</script>

<Dialog
	locked={busy}
	title={t(restore ? 'files.restoreTitle' : move ? 'files.moveTitle' : 'files.copyTitle', {
		count: paths.length,
	})}
	{onclose}
	wide
>
	<form onsubmit={submit}>
		<fieldset disabled={busy}>
			<section class="summary">
				<h3>{t('files.transferSummary')}</h3>
				<p>{t('files.source')}: <span class="mono">{from.name}</span></p>
				<ul>
					{#each paths as path}<li class="mono">{path}</li>{/each}
				</ul>
				<p>
					{t('files.copyTo')}:
					<span class="mono">{target?.name}/{restore ? restorePath : dir}</span>
				</p>
				{#if scope}<p>
						{t('files.summaryScope', { count: scope.files, size: formatBytes(scope.bytes) })}
					</p>{:else if scopeDeferred}<p class="muted" role="status">
						{t('files.scopeDeferred')}
					</p>{:else if scopeError}<p class="notice-error" role="alert">{scopeError}</p>{:else}<p
						role="status"
					>
						{t('files.measuring')}
					</p>{/if}
				{#if target}<p>{t('files.summaryFree', { size: formatBytes(target.freeBytes) })}</p>{/if}
				{#if scope && target && scope.bytes > target.freeBytes}<p class="notice-warn">
						{t('files.summarySpaceWarning')}
					</p>{/if}
				{#if nestedTarget}<p class="notice-error" role="alert">
						{t('files.summaryNested')}
					</p>{:else if sameFolder}<p class="notice-warn">
						{t(move ? 'files.summarySameMove' : 'files.summarySameCopy')}
					</p>{/if}
				<p class="muted">{t('files.summaryHint')}</p>
			</section>
			<label class="field-row">
				<span>{t('files.copyTo')}</span>
				<select
					class="field"
					value={toRoot}
					onchange={(event) => chooseRoot(event.currentTarget.value)}
				>
					{#each targets as root (root.id)}<option value={root.id}>{root.name}</option>{/each}
				</select>
			</label>
			<div class="crumbs mono" aria-label={t('files.targetFolder')}>
				<button type="button" onclick={() => (dir = '')}>/</button>
				{#each crumbs as crumb, index (index)}
					<span class="muted">/</span>
					<button type="button" onclick={() => (dir = crumbs.slice(0, index + 1).join('/'))}
						>{crumb}</button
					>
				{/each}
			</div>
			<div class="folders">
				{#each folders as name (name)}
					<button type="button" class="folder" onclick={() => (dir = join(dir, name))}
						><Icon name="folder" size={16} /><span>{name}</span></button
					>
				{:else}
					<p class="muted">{t('files.noSubfolders')}</p>
				{/each}
			</div>
			{#if restore}
				<p>{t('files.restoreHelp')}</p>
				<label class="field-row"
					><span>{t('files.restoreFolder')}</span><input
						class="field"
						bind:value={restoreName}
						required
						maxlength="200"
					/></label
				>
				<p class="mono">{targets.find((root) => root.id === toRoot)?.name}/{restorePath}</p>
				{#if created}<p>{t('files.restoreCreated', { path: created.path })}</p>{/if}
			{:else}
				<label class="field-row">
					<span>{t('files.conflict')}</span>
					<select
						class="field"
						aria-label={t('files.conflict')}
						bind:value={conflict}
						onchange={() => (replaceConfirmed = false)}
					>
						<option value="individual">{t('files.conflictIndividual')}</option>
						<option value="rename">{t('files.conflict.rename')}</option>
						<option value="skip">{t('files.conflict.skip')}</option>
						<option value="replace">{t('files.conflict.replace')}</option>
					</select>
				</label>
				{#if compared && compared.conflicts.length}
					<p>{t('files.conflictHelp')}</p>
					<div class="conflicts">
						<table>
							<thead
								><tr
									><th>{t('files.path')}</th><th>{t('files.incoming')}</th><th
										>{t('files.existing')}</th
									>{#if conflict === 'individual'}<th>{t('files.conflict')}</th>{/if}</tr
								></thead
							><tbody>
								{#each compared.conflicts as item (item.path)}<tr
										><td>{item.path}</td><td
											>{item.source.dir ? t('files.folderType') : formatBytes(item.source.size)}<br
											/>{formatDateTime(new Date(item.source.modified))}</td
										><td
											>{item.target.dir ? t('files.folderType') : formatBytes(item.target.size)}<br
											/>{formatDateTime(new Date(item.target.modified))}</td
										>{#if conflict === 'individual' && decisions[item.sourcePath ?? item.source.name]}<td
											>
												<select
													class="field"
													aria-label={t('files.choiceFor', { name: item.path })}
													bind:value={decisions[item.sourcePath ?? item.source.name].action}
													onchange={() => (replaceConfirmed = false)}
													><option value="skip">{t('files.conflict.skip')}</option><option
														value="replace"
														disabled={item.source.dir || item.target.dir}
														>{t('files.conflict.replace')}</option
													><option value="rename">{t('files.conflict.rename')}</option></select
												>
												{#if decisions[item.sourcePath ?? item.source.name].action === 'rename'}<input
														class="field"
														aria-label={t('files.newNameFor', { name: item.path })}
														bind:value={decisions[item.sourcePath ?? item.source.name].name}
													/>{/if}
											</td>{/if}</tr
									>{/each}
							</tbody>
						</table>
					</div>
					{#if compared.truncated}<p class="notice-warn">{t('files.conflictTruncated')}</p>{/if}
				{/if}
				{#if inspecting}<p role="status">{t('files.inspectTarget')}</p>{/if}
				{#if inspectionError}<p class="notice-error" role="alert">{inspectionError}</p>
					<button type="button" class="btn" disabled={inspecting} onclick={() => inspectTarget()}
						>{t('common.retry')}</button
					>{/if}
				{#if replaces}<label
						><input
							type="checkbox"
							bind:checked={replaceConfirmed}
							disabled={compared?.truncated || !compared}
						/>{t('files.replaceConfirm')}</label
					>{/if}
			{/if}
			{#if move && !restore}<p class="muted">{t('files.moveHint')}</p>{/if}
			{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={onclose}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn {replaces && !restore ? 'btn-danger' : 'btn-primary'}"
					disabled={busy ||
						inspecting ||
						(!scope && !scopeDeferred) ||
						nestedTarget ||
						(move && sameFolder) ||
						!toRoot ||
						invalidChoices ||
						(!restore &&
							((replaces && (!compared || !replaceConfirmed)) ||
								((replaces || conflict === 'individual') &&
									(compared?.truncated || !!inspectionError))))}
					>{t(restore ? 'files.restoreStart' : move ? 'files.moveHere' : 'files.copyHere')}</button
				>
			</div>
		</fieldset>
	</form>
</Dialog>

<style>
	.summary ul {
		max-height: 9rem;
		overflow: auto;
	}
	.summary {
		overflow-wrap: anywhere;
	}
	.conflicts {
		max-height: 18rem;
		overflow: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.8rem;
	}
	td,
	th {
		padding: 8px;
		text-align: left;
		border-bottom: 1px solid var(--border);
		overflow-wrap: anywhere;
		min-width: 120px;
	}
	fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		gap: 0.2rem;
		margin: 0.6rem 0 0.3rem;
	}
	.crumbs button {
		background: none;
		border: 0;
		color: var(--accent);
		cursor: pointer;
		font: inherit;
		padding: 0 0.1rem;
	}
	.folders {
		max-height: 14rem;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 0.3rem;
		margin-bottom: 0.8rem;
	}
	.folder {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 100%;
		min-height: 2rem;
		background: none;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
		font: inherit;
		padding: 0 0.5rem;
		text-align: left;
	}
	.folder:hover {
		background: var(--chip);
	}
</style>
