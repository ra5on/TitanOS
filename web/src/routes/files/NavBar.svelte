<script lang="ts">
	import { tick } from 'svelte';
	import { api, type FileEntry, type FileRoot } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { folderPath } from '#lib/file-actions.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { FileCommand } from './commands.ts';

	type Props = {
		root: FileRoot | undefined;
		rootId: string;
		dir: string;
		canBack: boolean;
		canForward: boolean;
		isFavorite: boolean;
		favoritesFull: boolean;
		search: string;
		/** The path is shown as a field to type in. */
		editing: boolean;
		/** Answer to the last path action, shown below the bar. */
		message: string;
		/** The typed path. */
		input: string;
		onopen: (root: string, path: string) => void;
		oncommand: (command: FileCommand) => void;
		onwindowspath: () => void;
		onreload: () => void;
		/** The search text changed. */
		onsearch: () => void;
	};
	let {
		root,
		rootId,
		dir,
		canBack,
		canForward,
		isFavorite,
		favoritesFull,
		search = $bindable(),
		editing = $bindable(),
		message = $bindable(),
		input = $bindable(),
		onopen,
		oncommand,
		onwindowspath,
		onreload,
		onsearch,
	}: Props = $props();

	let pathField = $state<HTMLInputElement>();
	const crumbs = $derived(dir ? dir.split('/') : []);

	export async function editPath() {
		input = '/' + dir;
		message = '';
		editing = true;
		await tick();
		pathField?.select();
	}
	async function goToPath(event: SubmitEvent) {
		event.preventDefault();
		const path = folderPath(input);
		if (path === null) {
			message = t('files.invalidPath');
			return;
		}
		const id = rootId;
		try {
			await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root: id, path }));
			if (rootId === id) {
				message = '';
				onopen(id, path);
			}
		} catch (err) {
			message = apiErrorText(err);
		}
	}
	async function copyPath() {
		try {
			await navigator.clipboard.writeText('/' + dir);
			message = t('files.pathCopied');
		} catch {
			message = t('files.pathCopyFailed');
		}
	}
</script>

<!-- Where: history, path and search. -->
<div class="toolbar nav-bar" class:path-editing={editing}>
	<div class="nav-buttons">
		<button
			type="button"
			class="btn icon"
			aria-label={t('files.back')}
			title={t('files.back')}
			disabled={!canBack}
			onclick={() => oncommand('back')}><Icon name="arrowLeft" size={16} /></button
		>
		<button
			type="button"
			class="btn icon"
			aria-label={t('files.forward')}
			title={t('files.forward')}
			disabled={!canForward}
			onclick={() => oncommand('forward')}><Icon name="arrowRight" size={16} /></button
		>
		<button
			type="button"
			class="btn icon"
			aria-label={t('files.up')}
			title={t('files.up')}
			disabled={!dir}
			onclick={() => oncommand('up')}><Icon name="arrowUp" size={16} /></button
		>
	</div>
	{#if editing && root}
		<form class="path-form" onsubmit={goToPath}>
			<label class="sr-only" for="file-path">{t('files.folderPath')}</label>
			<input
				id="file-path"
				class="field mono"
				bind:this={pathField}
				bind:value={input}
				placeholder="/"
				title={t('files.pathHint', { name: root.name })}
				autocomplete="off"
				spellcheck="false"
				onkeydown={(event) => {
					if (event.key === 'Escape') {
						event.preventDefault();
						editing = false;
						message = '';
					}
				}}
			/>
			<button class="btn">{t('files.goToPath')}</button>
			<button
				type="button"
				class="btn"
				onclick={() => {
					editing = false;
					message = '';
				}}>{t('common.cancel')}</button
			>
		</form>
	{:else}
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div
			class="crumbs mono"
			title={t('files.editPathHint')}
			onclick={(event) => {
				if (event.target === event.currentTarget) editPath();
			}}
		>
			{#if root}<button type="button" onclick={() => onopen(rootId, '')}>{root.name}</button>{/if}
			{#each crumbs as crumb, index (index)}
				<span class="muted" aria-hidden="true">/</span>
				<button type="button" onclick={() => onopen(rootId, crumbs.slice(0, index + 1).join('/'))}
					>{crumb}</button
				>
			{/each}
			{#if root}<button
					type="button"
					class="edit-path"
					aria-label={t('files.editPath')}
					title={t('files.editPath')}
					onclick={editPath}><Icon name="edit" size={14} /></button
				>{/if}
		</div>
	{/if}
	{#if root && root.kind !== 'stick'}
		<button
			type="button"
			class="btn icon favorite"
			aria-label={t(isFavorite ? 'files.unfavorite' : 'files.favorite')}
			title={`${t(isFavorite ? 'files.unfavorite' : 'files.favorite')} – ${t('files.favoriteAccount')}`}
			aria-pressed={isFavorite}
			disabled={!isFavorite && favoritesFull}
			onclick={() => oncommand('favorite')}><Icon name="star" size={16} /></button
		>
	{/if}
	{#if root}
		<Dropdown label={t('files.moreFolderActions')} icon="more" align="right">
			{#snippet children(close)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						editPath();
					}}><span>{t('files.editPath')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						copyPath();
					}}><span>{t('files.copyPath')}</span></button
				>
				{#if root?.kind === 'share' || root?.kind === 'volume'}<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							onwindowspath();
						}}><span>{t('files.windowsPath')}</span></button
					>{/if}
				<hr />
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						onreload();
					}}><span>{t('files.reload')}</span></button
				>
			{/snippet}
		</Dropdown>
	{/if}
	<label class="search">
		<Icon name="search" size={16} />
		<input
			class="field"
			type="search"
			aria-label={t('files.searchFolder')}
			placeholder={t('files.searchFolder')}
			bind:value={search}
			oninput={onsearch}
		/>
	</label>
</div>
{#if message}<p class="path-message" role="status">{message}</p>{/if}

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		border-bottom: 1px solid var(--panel-border);
	}
	.toolbar .btn {
		height: 2.375rem;
	}
	.btn.icon {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.btn[aria-pressed='true'] {
		border-color: var(--accent);
		color: var(--accent);
	}
	.nav-buttons {
		display: flex;
		gap: 4px;
	}
	.crumbs {
		flex: 1 1 240px;
		min-width: 0;
		min-height: 2.375rem;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px;
		padding: 0 4px;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		font-size: var(--text-md);
		cursor: text;
	}
	.crumbs:hover {
		border-color: var(--border);
	}
	.crumbs button {
		padding: 4px 6px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.crumbs button:hover {
		background: var(--chip);
	}
	.crumbs .edit-path {
		display: inline-flex;
		margin-left: auto;
		color: var(--muted);
	}
	.path-form {
		flex: 1 1 320px;
		min-width: 0;
		display: flex;
		gap: 6px;
	}
	.path-form input {
		flex: 1 1 160px;
		min-width: 0;
	}
	.path-message {
		margin: 0;
		padding: 6px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: var(--text-base);
	}
	.search {
		flex: 0 1 260px;
		min-width: 160px;
		position: relative;
		display: flex;
		align-items: center;
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
		color: var(--muted);
		pointer-events: none;
	}
	.search input {
		width: 100%;
		padding-left: 34px;
	}

	@media (max-width: 760px) {
		.toolbar .btn,
		.btn.icon {
			height: 2.75rem;
		}
		.btn.icon {
			width: 2.75rem;
		}
		.crumbs {
			order: 5;
			flex-basis: 100%;
		}
		.path-form {
			order: 5;
			flex-basis: 100%;
		}
		.search {
			flex: 1 1 100%;
			order: 6;
		}
		/* Phones have their own bar for this; only the path field stays. */
		.nav-bar {
			display: none;
		}
		.nav-bar.path-editing {
			display: flex;
		}
		.nav-bar.path-editing > :not(.path-form) {
			display: none;
		}
	}
</style>
