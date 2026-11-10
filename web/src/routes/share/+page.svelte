<script lang="ts">
	// PLAN 70: "TitanOS als Handy-App" – files shared from another app
	// (Android share sheet → installed web app → service worker, static/sw.js)
	// wait in this browser until they are uploaded to a folder chosen here.
	// The upload is the file manager's own (lib/uploads.svelte.ts): same
	// rights, same name rules; a name that exists gets a new one.
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, type FileRoot } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { enqueue, uploads, type Upload } from '#lib/uploads.svelte.ts';
	import FileDialog from '#lib/webapps/FileDialog.svelte';

	const CACHE = 'titanos-share';
	const LAST = 'titanos.share.target';
	type Shared = { key: string; name: string; size: number; type: string };

	let shared = $state<Shared[] | null>(null);
	let text = $state('');
	let saveText = $state(true);
	let target = $state<{ root: string; rootName: string; dir: string } | null>(null);
	let choosing = $state(false);
	let sent = $state<File[]>([]);
	let error = $state('');

	onMount(() => {
		void (async () => {
			try {
				const cache = await caches.open(CACHE);
				const res = await cache.match('/share-target/list');
				const list = res ? await res.json() : null;
				shared = Array.isArray(list?.files) ? list.files : [];
				text = typeof list?.text === 'string' ? list.text : '';
			} catch {
				shared = [];
			}
			try {
				const last = JSON.parse(localStorage.getItem(LAST) ?? 'null');
				if (last && typeof last.root === 'string' && typeof last.dir === 'string') {
					const roots = await api.get<FileRoot[]>('/files/roots');
					const root = roots.find((r) => r.id === last.root && r.writable);
					if (root) target = { root: root.id, rootName: root.name, dir: last.dir };
				}
			} catch {
				// No remembered folder: the user chooses one.
			}
		})();
	});

	const mine = $derived(uploads.items.filter((u: Upload) => sent.includes(u.file)));
	const finished = $derived(mine.length > 0 && mine.every((u) => u.state === 'done'));
	const failed = $derived(mine.filter((u) => u.state === 'failed'));
	const total = $derived((shared ?? []).reduce((n, f) => n + f.size, 0));
	const nothing = $derived(shared !== null && !shared.length && !text);

	async function choose(chosen: { root: string; path: string }) {
		choosing = false;
		const roots = await api.get<FileRoot[]>('/files/roots').catch(() => [] as FileRoot[]);
		const root = roots.find((r) => r.id === chosen.root);
		target = { root: chosen.root, rootName: root?.name ?? chosen.root, dir: chosen.path };
		try {
			localStorage.setItem(LAST, JSON.stringify({ root: chosen.root, dir: chosen.path }));
		} catch {
			// Not remembered in this browser.
		}
	}

	async function upload() {
		if (!target || !shared) return;
		error = '';
		try {
			const cache = await caches.open(CACHE);
			const files: File[] = [];
			for (const item of shared) {
				const res = await cache.match(item.key);
				if (!res) continue;
				files.push(new File([await res.blob()], item.name, { type: item.type }));
			}
			if (text && saveText) {
				const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-');
				files.push(new File([text + '\n'], `geteilt-${stamp}.txt`, { type: 'text/plain' }));
			}
			sent = files;
			enqueue(
				files.map((file) => ({ file, dir: target!.dir, conflict: 'rename' as const })),
				{ id: target.root, name: target.rootName },
			);
		} catch {
			error = t('share.readFailed');
		}
	}

	// Done: the shared files leave this browser.
	$effect(() => {
		if (finished) void caches.delete(CACHE);
	});

	async function drop() {
		await caches.delete(CACHE).catch(() => {});
		location.assign('/');
	}

	const filesLink = $derived(
		target ? '/files?' + new URLSearchParams({ root: target.root, path: target.dir }) : '/files',
	);
	const percent = (u: Upload) => (u.file.size ? Math.round((u.sent / u.file.size) * 100) : 100);
</script>

<PageHead title={t('share.title')} />

<div class="share">
	{#if shared === null}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else if nothing}
		<section class="tile ui-card">
			<div class="card-head"><h2>{t('share.nothing')}</h2></div>
			<p class="pad muted">
				{page.url.searchParams.has('missing') ? t('share.missing') : t('share.noneHint')}
			</p>
			<div class="card-foot"><a class="btn" href="/">{t('share.home')}</a></div>
		</section>
	{:else}
		<section class="tile ui-card" aria-labelledby="share-files">
			<div class="card-head">
				<h2 id="share-files">{t('share.files')}</h2>
				<span class="muted small">{formatBytes(total)}</span>
			</div>
			<ul class="list">
				{#each shared as f, i (f.key)}
					{@const u = mine.find((x) => x.file === sent[i])}
					<li class="set-row">
						<span class="set-label"
							><span class="name"><Icon name="file" size={16} />{f.name}</span><span
								class="set-hint">{formatBytes(f.size)}</span
							></span
						>
						<span class="set-control end"
							>{#if u}{u.state === 'done'
									? t('share.done')
									: u.state === 'failed'
										? u.error || t('share.failed')
										: percent(u) + ' %'}{/if}</span
						>
					</li>
				{/each}
				{#if text}
					<li class="set-row">
						<span class="set-label">{t('share.text')}<span class="set-hint text">{text}</span></span
						>
						<span class="set-control end"
							><input
								type="checkbox"
								role="switch"
								class="switch"
								aria-label={t('share.saveText')}
								bind:checked={saveText}
								disabled={sent.length > 0}
							/></span
						>
					</li>
				{/if}
			</ul>
		</section>

		<section class="tile ui-card" aria-labelledby="share-where">
			<div class="card-head"><h2 id="share-where">{t('share.where')}</h2></div>
			<div class="set-row">
				<span class="set-label"
					>{target
						? target.rootName + (target.dir ? ' / ' + target.dir : '')
						: t('share.noFolder')}</span
				>
				<span class="set-control end"
					><button
						type="button"
						class="btn"
						disabled={sent.length > 0}
						onclick={() => (choosing = true)}>{t('share.choose')}</button
					></span
				>
			</div>
			{#if error}<p class="notice-error pad" role="alert">{error}</p>{/if}
			{#if failed.length}<p class="notice-error pad" role="alert">
					{t('share.someFailed', { count: failed.length })}
				</p>{/if}
			<div class="card-foot">
				{#if finished}
					<a class="btn btn-primary" href={filesLink}>{t('share.show')}</a>
					<a class="btn" href="/">{t('share.home')}</a>
				{:else}
					<button type="button" class="btn" onclick={drop} disabled={sent.length > 0}
						>{t('share.drop')}</button
					>
					<button
						type="button"
						class="btn btn-primary"
						disabled={!target || sent.length > 0}
						onclick={upload}>{t('share.upload')}</button
					>
				{/if}
			</div>
		</section>
	{/if}
</div>
{#if shared?.length}<StatusBar label={t('share.statusBar')}>
		<span>{t('share.statFiles', { count: shared.length })} · {formatBytes(total)}</span>
		{#if sent.length}<span>{t('share.statSent')} <b>{sent.length}</b></span>{/if}
		{#if target}<span class="end">{target.rootName + (target.dir ? ' / ' + target.dir : '')}</span
			>{/if}
	</StatusBar>{/if}

{#if choosing}<FileDialog
		mode="folder"
		title={t('share.where')}
		start={target ? { root: target.root, dir: target.dir } : undefined}
		onpick={choose}
		onclose={() => (choosing = false)}
	/>{/if}

<style>
	.share {
		box-sizing: border-box;
		width: 100%;
		display: grid;
		gap: var(--gap, var(--space-4));
		max-width: 720px;
		margin: 0 auto;
		padding: var(--space-3, var(--space-3));
	}
	.list {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.list .set-row + .set-row {
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.name {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		overflow-wrap: anywhere;
	}
	.text {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.pad {
		margin: 0;
		padding: var(--space-3) var(--space-4);
	}
	.card-foot {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
</style>
