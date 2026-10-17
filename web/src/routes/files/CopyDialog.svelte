<script lang="ts">
	import { api, ApiError, type CopyJob, type FileEntry, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		roots: FileRoot[];
		/** The root and the paths (inside it) that are copied or moved. */
		from: FileRoot;
		paths: string[];
		move: boolean;
		/** Where the folder chooser starts. */
		startDir: string;
		onclose: () => void;
		ondone: (job: CopyJob) => void;
	};
	let { roots, from, paths, move, startDir, onclose, ondone }: Props = $props();

	const targets = $derived(roots.filter((root) => root.writable));
	// svelte-ignore state_referenced_locally
	let toRoot = $state(from.writable ? from.id : (targets[0]?.id ?? ''));
	// svelte-ignore state_referenced_locally
	let dir = $state(from.writable ? startDir : '');
	let folders = $state<string[]>([]);
	let conflict = $state('rename');
	let error = $state('');
	let busy = $state(false);

	const crumbs = $derived(dir ? dir.split('/') : []);
	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	async function load() {
		try {
			const entries = await api.get<FileEntry[]>(`/files/list?root=${encodeURIComponent(toRoot)}&path=${encodeURIComponent(dir)}`);
			// A folder cannot go into itself.
			folders = entries.filter((entry) => entry.dir && !(toRoot === from.id && paths.includes(join(dir, entry.name)))).map((entry) => entry.name);
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

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		try {
			ondone(await api.post<CopyJob>('/files/copy', { root: from.id, paths, toRoot, toDir: dir, move, conflict }));
		} catch (err) {
			error = fail(err);
			busy = false;
		}
	}
</script>

<Dialog title={t(move ? 'files.moveTitle' : 'files.copyTitle', { count: paths.length })} {onclose} wide>
	<form onsubmit={submit}>
		<label class="field-row">
			<span>{t('files.copyTo')}</span>
			<select class="field" value={toRoot} onchange={(event) => chooseRoot(event.currentTarget.value)}>
				{#each targets as root (root.id)}<option value={root.id}>{root.name}</option>{/each}
			</select>
		</label>
		<div class="crumbs mono" aria-label={t('files.targetFolder')}>
			<button type="button" onclick={() => (dir = '')}>/</button>
			{#each crumbs as crumb, index (index)}
				<span class="muted">/</span>
				<button type="button" onclick={() => (dir = crumbs.slice(0, index + 1).join('/'))}>{crumb}</button>
			{/each}
		</div>
		<div class="folders">
			{#each folders as name (name)}
				<button type="button" class="folder" onclick={() => (dir = join(dir, name))}><Icon name="folder" size={16} /><span>{name}</span></button>
			{:else}
				<p class="muted">{t('files.noSubfolders')}</p>
			{/each}
		</div>
		<label class="field-row">
			<span>{t('files.conflict')}</span>
			<select class="field" bind:value={conflict}>
				<option value="rename">{t('files.conflict.rename')}</option>
				<option value="skip">{t('files.conflict.skip')}</option>
				<option value="replace">{t('files.conflict.replace')}</option>
			</select>
		</label>
		{#if move}<p class="muted">{t('files.moveHint')}</p>{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={busy || !toRoot}>{t(move ? 'files.moveHere' : 'files.copyHere')}</button>
		</div>
	</form>
</Dialog>

<style>
	.crumbs { display: flex; flex-wrap: wrap; gap: 0.2rem; margin: 0.6rem 0 0.3rem; }
	.crumbs button { background: none; border: 0; color: var(--accent); cursor: pointer; font: inherit; padding: 0 0.1rem; }
	.folders { max-height: 14rem; overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-ctl); padding: 0.3rem; margin-bottom: 0.8rem; }
	.folder { display: flex; align-items: center; gap: 0.5rem; width: 100%; min-height: 2rem; background: none; border: 0; border-radius: var(--radius-ctl); color: var(--text); cursor: pointer; font: inherit; padding: 0 0.5rem; text-align: left; }
	.folder:hover { background: var(--chip); }
</style>
