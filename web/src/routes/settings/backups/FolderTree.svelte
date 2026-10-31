<script lang="ts">
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		share: string;
		/** Set: folders of this volume outside the shares, instead of a share. */
		volume?: string;
		/** all: everything is saved, unchecking leaves a folder out. only: nothing is, checking adds a folder. */
		mode: 'all' | 'only';
		/** Folders saved in mode only, relative to the share. */
		folders: string[];
		/** Folders left out, as share/folder. */
		skip: string[];
		onchange: (folders: string[], skip: string[]) => void;
	};
	let { share, volume, mode, folders, skip, onchange }: Props = $props();

	let children = $state<Record<string, string[]>>({});
	let open = $state<Record<string, boolean>>({});
	let failed = $state<Record<string, string>>({});

	const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name);
	const listed = $derived(
		mode === 'only'
			? folders
			: skip.filter((s) => s.startsWith(`${share}/`)).map((s) => s.slice(share.length + 1)),
	);
	const under = (path: string, parent: string) => path.startsWith(`${parent}/`);
	/** A folder above this one is already chosen (mode only) or left out (mode all). */
	const inherited = (path: string) => listed.some((p) => under(path, p));
	const checked = (path: string) =>
		mode === 'only'
			? listed.includes(path) || inherited(path)
			: !listed.includes(path) && !inherited(path);

	function toggle(path: string) {
		if (inherited(path)) return;
		const own = listed.includes(path);
		// Choosing or leaving out a folder covers what is below it.
		const next = own
			? listed.filter((p) => p !== path)
			: [...listed.filter((p) => !under(p, path)), path];
		if (mode === 'only') onchange(next, skip);
		else
			onchange(folders, [
				...skip.filter((s) => !s.startsWith(`${share}/`)),
				...next.map((p) => `${share}/${p}`),
			]);
	}

	async function expand(dir: string) {
		open[dir] = !open[dir];
		if (!open[dir] || children[dir]) return;
		try {
			const res = await api.get<{ folders: string[] }>(
				volume
					? `/backups/folders?volume=${encodeURIComponent(volume)}&dir=${encodeURIComponent(dir)}`
					: `/backups/folders?share=${encodeURIComponent(share)}&dir=${encodeURIComponent(dir)}`,
			);
			children[dir] = res.folders;
			delete failed[dir];
		} catch (e) {
			failed[dir] = apiErrorText(e);
		}
	}

	expand('');
</script>

{#snippet level(dir: string, depth: number)}
	{#if failed[dir]}<p class="notice-error" role="alert">{failed[dir]}</p>{/if}
	{#if children[dir]?.length === 0 && dir === ''}<p class="muted">{t('backup.noFolders')}</p>{/if}
	{#each children[dir] ?? [] as name (name)}
		{@const path = join(dir, name)}
		<div class="node" style:padding-left="{depth * 1.25}rem">
			<button
				type="button"
				class="arrow"
				aria-expanded={!!open[path]}
				aria-label={t('backup.openFolder', { name })}
				onclick={() => expand(path)}>{open[path] ? '▾' : '▸'}</button
			>
			<label class="check"
				><input
					type="checkbox"
					checked={checked(path)}
					disabled={inherited(path)}
					onchange={() => toggle(path)}
				/><span class="mono">{name}</span></label
			>
		</div>
		{#if open[path]}{@render level(path, depth + 1)}{/if}
	{/each}
{/snippet}

<div class="tree" role="group" aria-label={t('backup.foldersOf', { share })}>
	{@render level('', 0)}
</div>

<style>
	.tree {
		max-height: 18rem;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 0.4rem 0.6rem;
	}
	.node {
		display: flex;
		align-items: center;
		gap: 0.25rem;
		min-height: 1.8rem;
	}
	.arrow {
		background: none;
		border: 0;
		color: var(--muted);
		cursor: pointer;
		width: 1.4rem;
		padding: 0;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
</style>
