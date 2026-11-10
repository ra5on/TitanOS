<script lang="ts">
	// An open folder of the desktop: its programs in a small panel.
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { folderOf, takeOut as takeFromFolder, type IconsNow } from './desk-ops.ts';
	import type { Program } from './programs.svelte.ts';

	let {
		openFolder = $bindable(),
		renaming = $bindable(),
		byKey,
		iconsNow,
		open,
	}: {
		openFolder: string | null;
		renaming: string | null;
		byKey: Map<string, Program>;
		iconsNow: IconsNow;
		open: (key: string, event?: Event, href?: string) => void;
	} = $props();

	const folder = $derived(openFolder ? folderOf(openFolder) : undefined);
	const takeOut = (folderKey: string, app: string) => takeFromFolder(folderKey, app, iconsNow);
</script>

{#if folder}
	<div class="folder-pop glass-panel" role="dialog" aria-label={folder.name}>
		<div class="folder-head">
			<b>{folder.name}</b>
			<button type="button" class="link" onclick={() => (renaming = openFolder)}
				>{t('desktop.folder.rename')}</button
			>
			<button
				type="button"
				class="pb"
				onclick={() => (openFolder = null)}
				aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
			>
		</div>
		<div class="folder-grid">
			{#each folder.apps as app (app)}
				{@const fp = byKey.get(app)}
				{#if fp}
					<button
						type="button"
						class="ico small"
						onclick={(e) => open(app, e)}
						oncontextmenu={(e) => {
							e.preventDefault();
							takeOut(openFolder ?? '', app);
						}}
						title={t('desktop.folder.takeOutHint')}
					>
						<span class="ico-wrap"
							><DesktopIcon
								glyph={fp.glyph}
								gradient={fp.gradient}
								image={fp.image}
								name={fp.label}
								size={52}
							/>{#if fp.badge?.count}<span class="badge count">{fp.badge.count}</span>{/if}</span
						>
						<span class="label" data-cut="excerpt">{fp.label}</span>
					</button>
				{/if}
			{:else}
				<p class="muted">{t('desktop.folder.empty')}</p>
			{/each}
		</div>
	</div>
{/if}

<style>
	.folder-pop {
		position: absolute;
		left: 50%;
		top: 18%;
		transform: translateX(-50%);
		width: min(460px, calc(100% - 40px));
		border-radius: 22px;
		padding: var(--space-4) 18px;
		z-index: 8800;
	}
	.folder-head {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		margin-bottom: var(--space-3);
	}
	.folder-head b {
		flex: 1;
		font-size: var(--text-lg);
	}
	.folder-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: var(--space-2-5);
	}
	.ico {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 7px;
		padding: var(--space-1-5) 2px;
		border: 0;
		border-radius: 12px;
		background: none;
		color: #fff;
		font: inherit;
		font-size: var(--text-md);
		font-weight: 500;
		line-height: 1.2;
		text-align: center;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.65);
		cursor: pointer;
		height: fit-content;
	}
	.ico:hover {
		background: color-mix(in srgb, #fff 12%, transparent);
	}
	.ico:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.ico .label {
		max-width: 100%;
		overflow-wrap: break-word;
		hyphens: auto;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.ico-wrap {
		position: relative;
		display: inline-block;
	}
	.badge {
		position: absolute;
		top: -6px;
		right: -8px;
		min-width: 22px;
		height: 22px;
		padding: 0 var(--space-1-5);
		border-radius: 11px;
		display: grid;
		place-items: center;
		font-size: 12px;
		font-weight: 700;
		color: #fff;
		text-shadow: none;
		box-shadow: 0 0 0 2px rgb(0 0 0 / 0.35);
	}
	.badge.count {
		background: #d93a3f;
	}
	.ico.small {
		color: var(--text);
		text-shadow: none;
		font-size: var(--text-sm);
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.muted {
		color: var(--muted);
	}
	.pb {
		width: var(--pb);
		height: var(--pb);
		flex: none;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.pb:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.pb:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
</style>
