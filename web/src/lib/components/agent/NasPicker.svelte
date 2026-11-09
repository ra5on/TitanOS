<script lang="ts">
	// Files of the NAS for the chat (PLAN 47): the shares the account sees
	// in the file manager, with the same rights. Chosen files go into the
	// message as [Datei: root/path]; nothing is copied.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api, type FileEntry, type FileRoot } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let {
		onpick,
		onclose,
	}: {
		/** The chosen files as root:path. */
		onpick: (refs: string[]) => void;
		onclose: () => void;
	} = $props();

	type Row = FileEntry & { path: string };
	const MAX = 10;

	let roots = $state<FileRoot[] | null>(null);
	let root = $state<FileRoot | null>(null);
	let dir = $state('');
	let rows = $state<Row[] | null>(null);
	let query = $state('');
	let found = $state(false);
	let chosen = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);

	const join = (a: string, b: string) => (a ? `${a}/${b}` : b);
	const ref = (r: Row) => `${root!.id}:${r.path}`;

	$effect(() => {
		void (async () => {
			try {
				const list = await api.get<FileRoot[]>('/files/roots');
				roots = list.filter((r) => r.kind !== 'stick');
			} catch (e) {
				error = apiErrorText(e);
				roots = [];
			}
		})();
	});

	async function go(next: FileRoot | null, path = '') {
		error = '';
		query = '';
		found = false;
		root = next;
		dir = path;
		rows = null;
		if (!next) return;
		busy = true;
		try {
			const list = await api.get<FileEntry[]>(
				`/files/list?root=${encodeURIComponent(next.id)}&path=${encodeURIComponent(path)}`,
			);
			rows = list
				.filter((e) => !e.name.startsWith('.'))
				.sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name))
				.map((e) => ({ ...e, path: join(path, e.name) }));
		} catch (e) {
			error = apiErrorText(e);
			rows = [];
		} finally {
			busy = false;
		}
	}

	async function search(event: SubmitEvent) {
		event.preventDefault();
		if (!root) return;
		if (!query.trim()) return go(root, dir);
		busy = true;
		error = '';
		try {
			const result = await api.post<{ entries: Row[] }>('/files/search', {
				root: root.id,
				path: dir,
				query: query.trim(),
			});
			rows = result.entries.filter((e) => !e.dir).slice(0, 200);
			found = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	function toggle(r: Row) {
		const id = ref(r);
		if (chosen.includes(id)) chosen = chosen.filter((x) => x !== id);
		else if (chosen.length < MAX) chosen = [...chosen, id];
	}
	const crumbs = $derived(dir ? dir.split('/') : []);
</script>

<Dialog title={t('agent.nasTitle')} {onclose}>
	{#if root}
		<form class="search" onsubmit={search}>
			<Icon name="search" size={16} />
			<input
				class="field"
				type="search"
				bind:value={query}
				placeholder={t('agent.nasSearch', { name: crumbs.at(-1) ?? root.name })}
				aria-label={t('agent.nasSearch', { name: crumbs.at(-1) ?? root.name })}
			/>
		</form>
	{/if}
	<nav class="crumbs" aria-label={t('agent.nasWhere')}>
		<button type="button" class="link" onclick={() => go(null)}>{t('agent.nasShares')}</button>
		{#if root}
			› <button type="button" class="link" onclick={() => go(root, '')}>{root.name}</button>
			{#each crumbs as part, i (i)}
				› <button
					type="button"
					class="link"
					onclick={() => go(root, crumbs.slice(0, i + 1).join('/'))}>{part}</button
				>
			{/each}
		{/if}
	</nav>
	<ul class="list" aria-busy={busy}>
		{#if !root}
			{#if roots === null}
				<li class="muted pad">{t('common.loading')}</li>
			{:else}
				{#each roots as r (r.id)}
					<li>
						<button type="button" class="row" onclick={() => go(r)}>
							<span class="ic dir"><Icon name="folder" size={20} /></span>
							<span class="name"
								>{r.name}<small>{t(`agent.nasKind.${r.kind}` as 'agent.nasKind.share')}</small
								></span
							>
							<Icon name="chevronRight" size={16} />
						</button>
					</li>
				{:else}
					<li class="muted pad">{t('agent.nasNone')}</li>
				{/each}
			{/if}
		{:else if rows === null}
			<li class="muted pad">{t('common.loading')}</li>
		{:else}
			{#each rows as r (r.path)}
				<li>
					{#if r.dir}
						<button type="button" class="row" onclick={() => go(root, r.path)}>
							<span class="box-space"></span>
							<span class="ic dir"><Icon name="folder" size={20} /></span>
							<span class="name">{r.name}</span>
							<Icon name="chevronRight" size={16} />
						</button>
					{:else}
						<label class="row">
							<input
								type="checkbox"
								checked={chosen.includes(ref(r))}
								disabled={!chosen.includes(ref(r)) && chosen.length >= MAX}
								onchange={() => toggle(r)}
							/>
							<span class="ic"><Icon name="file" size={20} /></span>
							<span class="name"
								>{found ? r.path : r.name}<small
									>{formatBytes(r.size)} · {new Date(r.modified).toLocaleDateString()}</small
								></span
							>
						</label>
					{/if}
				</li>
			{:else}
				<li class="muted pad">{found ? t('agent.nasNoHits') : t('agent.nasEmpty')}</li>
			{/each}
		{/if}
	</ul>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<span class="muted note"
			>{chosen.length ? t('agent.nasChosen', { count: chosen.length }) : t('agent.nasRights')}</span
		>
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button
			type="button"
			class="btn btn-primary"
			disabled={!chosen.length}
			onclick={() => onpick(chosen)}
			>{chosen.length
				? t('agent.nasTake', { count: chosen.length })
				: t('agent.nasTakeNone')}</button
		>
	</div>
</Dialog>

<style>
	.search {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
	}
	.search input {
		flex: 1;
	}
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		align-items: center;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.link {
		background: none;
		border: 0;
		padding: 2px 0;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 12px;
		max-height: min(50dvh, 420px);
		overflow: auto;
	}
	.list li + li {
		border-top: 1px solid var(--border);
	}
	.pad {
		padding: var(--space-3);
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 48px;
		padding: var(--space-1) var(--space-3);
		background: none;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row input {
		width: 20px;
		height: 20px;
		flex: none;
		margin: 0;
	}
	.box-space {
		width: 20px;
		flex: none;
	}
	.ic {
		display: grid;
		color: var(--muted);
		flex: none;
	}
	.ic.dir {
		color: var(--accent);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		display: flex;
		flex-direction: column;
	}
	.name small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.note {
		flex: 1 1 100%;
		text-align: left;
		font-size: var(--text-sm);
		min-width: 10rem;
	}
</style>
