<script lang="ts">
	// Settings › Backups › job: one folder browser for everything (decided
	// 31.10.2026). It starts with the volumes; shares are simply folders on
	// their volume. Private data, snapshots and recycle bins are not listed.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { Chosen } from './selection.ts';

	let {
		volumes,
		shares,
		start,
		onpick,
		onclose,
	}: {
		volumes: string[];
		shares: { name: string; volume: string }[];
		start?: Chosen;
		onpick: (chosen: Chosen) => void;
		onclose: () => void;
	} = $props();

	// Where the browser stands: no volume yet, or a volume and a path on it.
	// The browser opens where the row stands; later changes of start do not move it.
	const opening = (() => start)();
	let volume = $state(opening?.volume ?? '');
	let path = $state(opening?.path ?? '');
	let folders = $state<string[] | null>(null);
	let error = $state('');

	const parts = $derived(path ? path.split('/') : []);
	const isShare = (name: string) =>
		path === '' && shares.some((s) => s.volume === volume && s.name === name);

	async function load() {
		folders = null;
		error = '';
		if (!volume) return;
		try {
			folders = (
				await api.get<{ folders: string[] }>(
					`/backups/folders?volume=${encodeURIComponent(volume)}&dir=${encodeURIComponent(path)}`,
				)
			).folders;
		} catch (e) {
			error = apiErrorText(e);
			folders = [];
		}
	}
	$effect(() => {
		void volume;
		void path;
		void load();
	});

	function go(nextVolume: string, nextPath: string) {
		volume = nextVolume;
		path = nextPath;
	}
	const join = (name: string) => (path ? `${path}/${name}` : name);
</script>

<Dialog title={t('backup.pick.title')} {onclose}>
	<nav class="crumbs" aria-label={t('backup.pick.where')}>
		<button type="button" class="link" onclick={() => go('', '')}>{t('backup.pick.volumes')}</button
		>{#if volume}<span aria-hidden="true">›</span><button
				type="button"
				class="link"
				onclick={() => go(volume, '')}>{volume}</button
			>{/if}{#each parts as part, i (i)}<span aria-hidden="true">›</span><button
				type="button"
				class="link"
				onclick={() => go(volume, parts.slice(0, i + 1).join('/'))}>{part}</button
			>{/each}
	</nav>
	<ul class="list">
		{#if !volume}
			{#each volumes as v (v)}
				<li>
					<button type="button" class="row" onclick={() => go(v, '')}
						><Icon name="disk" /> {t('backup.pick.volume', { volume: v })}</button
					>
				</li>
			{:else}
				<li class="muted">{t('backup.noVolume')}</li>
			{/each}
		{:else if folders === null}
			<li class="muted">{t('common.loading')}</li>
		{:else}
			{#each folders as name (name)}
				<li>
					<button type="button" class="row" onclick={() => go(volume, join(name))}
						><Icon name="folder" />
						{name}{#if isShare(name)}<span class="chip">{t('backup.pick.share')}</span>{/if}</button
					>
				</li>
			{:else}
				<li class="muted">{t('backup.pick.empty')}</li>
			{/each}
		{/if}
	</ul>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button
			type="button"
			class="btn btn-primary"
			disabled={!volume}
			onclick={() => onpick({ volume, path })}
			>{path
				? t('backup.pick.this', { name: parts[parts.length - 1] })
				: volume
					? t('backup.pick.wholeVolume', { volume })
					: t('backup.pick.chooseVolume')}</button
		>
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
	</div>
</Dialog>

<style>
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		align-items: center;
		margin-bottom: 8px;
	}
	.link {
		background: none;
		border: 0;
		padding: 2px 4px;
		color: var(--accent);
		cursor: pointer;
		font: inherit;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 50vh;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: 8px;
	}
	.list li + li {
		border-top: 1px solid var(--border);
	}
	.list .muted {
		padding: 10px 12px;
	}
	.row {
		display: flex;
		gap: 8px;
		align-items: center;
		width: 100%;
		padding: 10px 12px;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row:hover,
	.row:focus-visible {
		background: var(--hover, color-mix(in srgb, var(--accent) 10%, transparent));
	}
	.chip {
		margin-left: auto;
	}
</style>
