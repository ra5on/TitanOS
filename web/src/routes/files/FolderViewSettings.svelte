<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { layoutLabel, layouts, type MobileViews, type ViewPlace } from './mobile-views.svelte.ts';

	let { views }: { views: MobileViews } = $props();
	function pathParts(folder: ViewPlace) {
		const [kind, ...name] = folder.root.split('/');
		const group = t(
			kind === 'share'
				? 'files.shares'
				: kind === 'volume'
					? 'files.volumes'
					: kind === 'usb'
						? 'files.usb'
						: 'files.stick',
		);
		return [folder.node, group, ...name, ...folder.path.split('/')].filter(Boolean) as string[];
	}
	const folders = $derived(
		(views.data?.folders ?? []).toSorted((a, b) =>
			[a.node, a.root, a.path]
				.join('/')
				.localeCompare([b.node, b.root, b.path].join('/'), i18n.locale, { numeric: true }),
		),
	);
</script>

<section class="folder-settings" aria-labelledby="folder-views-heading" id="folder-views">
	<h2 id="folder-views-heading">{t('files.mobile.views')}</h2>
	{#if views.data}
		<label class="default-row"
			><span
				><strong>{t('files.mobile.default')}</strong>
				<small>{t('files.mobile.defaultHint')}</small></span
			>
			<select
				class="field"
				value={views.data.default}
				disabled={views.saving}
				onchange={(event) => {
					const next = layouts.find((l) => l.value === event.currentTarget.value);
					if (next) void views.change(next.value);
				}}
			>
				{#each layouts as layout}<option value={layout.value}>{t(layout.label)}</option>{/each}
			</select>
		</label>
		<h3>{t('files.mobile.exceptions')} <span>{folders.length}</span></h3>
		<ul>
			{#each folders as folder (JSON.stringify([folder.node, folder.root, folder.path]))}
				{@const parts = pathParts(folder)}
				<li>
					<span class="symbol"><Icon name="folder" size={23} /></span>
					<div>
						<strong>{parts.at(-1)}</strong>
						<small>{parts.slice(0, -1).join(' / ')}</small>
						<span class="view-name">{layoutLabel(folder.view)}</span>
					</div>
					<button
						class="btn"
						disabled={views.saving}
						aria-label={t('files.mobile.resetFolder', {
							path: parts.join(' / '),
						})}
						onclick={() => views.change('inherit', folder)}>{t('files.mobile.reset')}</button
					>
				</li>
			{:else}<li class="empty">{t('files.mobile.noExceptions')}</li>{/each}
		</ul>
	{:else if !views.error}<p role="status">{t('common.loading')}</p>{/if}
	{#if views.error}<p class="notice-error" role="alert">
			{views.error}
			{#if !views.data}<button class="btn" onclick={() => views.load()}>{t('common.retry')}</button
				>{/if}
		</p>{/if}
	{#if views.success}<p class="saved" role="status">
			<Icon name="check" size={16} />{views.success}
		</p>{/if}
</section>

<style>
	.folder-settings {
		margin-block-end: var(--space-5);
		color: var(--text);
	}
	h2 {
		font-size: clamp(1.3rem, 5vw, 1.65rem);
		margin: 0 0 0.85rem;
		letter-spacing: -0.03em;
	}
	h3 {
		font-size: 1rem;
		margin: 1.25rem 0 0.65rem;
	}
	h3 span {
		color: var(--muted);
		font-weight: 400;
		margin-inline-start: 0.25rem;
	}
	.default-row,
	ul {
		background: var(--nord-surface, var(--panel-bg));
		border: 1px solid var(--nord-line, var(--panel-border));
		border-radius: var(--nord-radius, 18px);
		-webkit-backdrop-filter: var(--nord-blur, var(--panel-backdrop));
		backdrop-filter: var(--nord-blur, var(--panel-backdrop));
	}
	.default-row,
	li {
		display: flex;
		gap: 0.7rem;
		align-items: center;
		padding: 0.85rem;
	}
	.default-row > span,
	li > div {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	small {
		display: block;
		margin: 0.12rem 0;
		font-size: 0.8rem;
		color: var(--muted);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li + li {
		border-top: 1px solid var(--nord-line, var(--panel-border));
	}
	.symbol,
	.saved {
		color: var(--accent);
	}
	.view-name {
		font-size: 0.8rem;
	}
	select {
		width: auto;
		max-width: 48%;
		min-height: 44px;
	}
	button {
		min-height: 44px;
	}
	.empty {
		color: var(--muted);
	}
	.saved {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
</style>
