<script lang="ts">
	// Chooses an installation medium: folders of the mounted volumes to click
	// through, or a search over the ISO files the NAS found on them.
	import { onMount, untrack } from 'svelte';
	import { api, type FileEntry } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { isISO, isoLocation, isoPath, matchMedia } from '#lib/iso-browser.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';

	type Props = {
		title: string;
		text?: string;
		initial?: string;
		submit: string;
		busy?: boolean;
		error?: string;
		onpick: (path: string) => void;
		onclose: () => void;
	};
	let {
		title,
		text = '',
		initial = '',
		submit,
		busy = false,
		error = '',
		onpick,
		onclose,
	}: Props = $props();

	let volumes = $state<string[]>([]);
	let volume = $state('');
	let dir = $state('');
	let entries = $state<FileEntry[] | null>(null);
	let media = $state<{ path: string; sizeBytes: number }[] | null>(null);
	let query = $state('');
	let chosen = $state(untrack(() => initial));
	let loadError = $state('');
	let serial = 0;

	const crumbs = $derived(dir.split('/').filter(Boolean));
	const found = $derived(media && query.trim() ? matchMedia(media, query) : []);

	onMount(async () => {
		api.get<{ media: { path: string; sizeBytes: number }[] }>('/vms/media').then(
			(result) => (media = result.media),
			() => (media = []),
		);
		try {
			const roots = await api.get<{ id: string; name: string; kind: string }[]>('/files/roots');
			volumes = roots.filter((r) => r.kind === 'volume').map((r) => r.name);
		} catch (err) {
			loadError = apiErrorText(err);
			return;
		}
		const at = isoLocation(initial, volumes);
		if (at) await open(at.volume, at.dir);
		else if (volumes.length) {
			// New volumes get an "iso" folder; start there when it exists.
			if (!(await open(volumes[0], 'iso', true))) await open(volumes[0], '');
		}
	});

	async function open(next: string, path: string, quiet = false): Promise<boolean> {
		const mine = ++serial;
		loadError = '';
		try {
			const list = await api.get<FileEntry[]>(
				`/files/list?${new URLSearchParams({ root: `volume/${next}`, path })}`,
			);
			if (mine !== serial) return true;
			volume = next;
			dir = path;
			entries = list
				.filter((e) => !e.name.startsWith('.') && (e.dir || isISO(e.name)))
				.sort(
					(a, b) =>
						Number(b.dir) - Number(a.dir) ||
						a.name.localeCompare(b.name, undefined, { numeric: true }),
				);
			return true;
		} catch (err) {
			if (!quiet && mine === serial) loadError = apiErrorText(err);
			return false;
		}
	}
	const into = (name: string) => (dir ? `${dir}/${name}` : name);
	const up = () => open(volume, crumbs.slice(0, -1).join('/'));
	function pick(path: string) {
		if (!busy && path) onpick(path);
	}
</script>

<Dialog {title} wide locked={busy} {onclose}>
	<div class="picker">
		{#if text}<p class="text">{text}</p>{/if}
		<label class="search"
			><Icon name="search" size={14} /><input
				class="field"
				type="search"
				bind:value={query}
				placeholder={t('vms.iso.search')}
				aria-label={t('vms.iso.search')}
			/></label
		>
		{#if query.trim()}
			<ul class="list" aria-label={t('vms.new.mediumFound')}>
				{#if media === null}<li class="muted small">{t('vms.ov.cdSearching')}</li>
				{:else}
					{#each found as entry (entry.path)}<li>
							<button
								type="button"
								class="row"
								class:selected={chosen === entry.path}
								onclick={() => (chosen = entry.path)}
								ondblclick={() => pick(entry.path)}
								><Icon name="disc" size={16} /><span class="name mono">{entry.path}</span><span
									class="muted small">{formatBytes(entry.sizeBytes)}</span
								></button
							>
						</li>
					{:else}<li class="muted small">{t('vms.iso.noMatch')}</li>{/each}
				{/if}
			</ul>
		{:else}
			{#if volumes.length > 1}<div class="volumes" role="group" aria-label={t('vms.iso.volumes')}>
					{#each volumes as name (name)}<button
							type="button"
							class="btn small-btn"
							class:btn-primary={name === volume}
							aria-pressed={name === volume}
							onclick={() => open(name, '')}><Icon name="disk" size={14} />{name}</button
						>{/each}
				</div>{/if}
			{#if volume}<nav class="where" aria-label={t('vms.iso.folder')}>
					<button
						type="button"
						class="btn small-btn"
						disabled={!dir}
						onclick={up}
						aria-label={t('vms.iso.up')}><Icon name="arrowUp" size={14} /></button
					><button type="button" class="crumb mono" onclick={() => open(volume, '')}
						>{volume}</button
					>{#each crumbs as crumb, i}<span class="muted">/</span><button
							type="button"
							class="crumb mono"
							onclick={() => open(volume, crumbs.slice(0, i + 1).join('/'))}>{crumb}</button
						>{/each}
				</nav>{/if}
			<ul class="list">
				{#if !volumes.length && !loadError}<li class="muted small">
						{t('vms.iso.noVolumes')}
					</li>
				{:else if entries}
					{#each entries as entry (entry.name)}<li>
							{#if entry.dir}<button
									type="button"
									class="row"
									onclick={() => open(volume, into(entry.name))}
									><Icon name="folder" size={16} /><span class="name">{entry.name}</span></button
								>
							{:else}{@const path = isoPath(volume, dir, entry.name)}<button
									type="button"
									class="row"
									class:selected={chosen === path}
									onclick={() => (chosen = path)}
									ondblclick={() => pick(path)}
									><Icon name="disc" size={16} /><span class="name">{entry.name}</span><span
										class="muted small">{formatBytes(entry.size)}</span
									></button
								>{/if}
						</li>
					{:else}<li class="muted small">{t('vms.iso.empty')}</li>{/each}
				{/if}
			</ul>
		{/if}
		{#if loadError}<p class="notice-error" role="alert">
				{loadError}
				{#if volume}<button type="button" class="btn small-btn" onclick={() => open(volume, dir)}
						>{t('common.retry')}</button
					>{/if}
			</p>{/if}
		<p class="chosen">
			<span class="muted">{t('vms.iso.chosen')}</span>
			<span class="mono">{chosen || '–'}</span>
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={busy}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !chosen}
				onclick={() => pick(chosen)}>{submit}</button
			>
		</div>
	</div>
</Dialog>

<style>
	.picker {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.text,
	.chosen {
		margin: 0;
		font-size: var(--text-base);
	}
	.chosen {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		overflow-wrap: anywhere;
	}
	.small {
		font-size: var(--text-sm);
	}
	.search {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.search input {
		flex: 1;
		min-width: 0;
	}
	.volumes,
	.where {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 6px;
		font-size: var(--text-base);
	}
	.small-btn {
		gap: 4px;
		height: 28px;
		padding: 0 10px;
		font-size: var(--text-sm);
	}
	.crumb {
		padding: 2px 4px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font-size: var(--text-base);
		cursor: pointer;
	}
	.crumb:hover {
		background: var(--chip);
	}
	.list {
		display: grid;
		min-height: 120px;
		max-height: 50vh;
		overflow-y: auto;
		margin: 0;
		padding: 0;
		list-style: none;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		align-content: start;
	}
	.list li {
		border-bottom: 1px solid var(--border);
	}
	.list li:last-child {
		border-bottom: 0;
	}
	.list > .muted {
		padding: 10px 12px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 8px 12px;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.row .name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.row:hover {
		background: var(--chip);
	}
	.row.selected {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		box-shadow: inset 3px 0 0 var(--accent);
	}
</style>
