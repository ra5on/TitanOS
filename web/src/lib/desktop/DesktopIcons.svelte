<script lang="ts">
	// The icons on the desktop: programs and folders on their grid, dragged to
	// another place or onto a folder, with the grid shown while dragging.
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import {
		folderOf,
		menuAt,
		renameFolder as saveFolderName,
		type Cell,
		type DeskMenu,
	} from './desk-ops.ts';
	import type { Program } from './programs.svelte.ts';

	let {
		menu = $bindable(),
		renaming = $bindable(),
		dragKey = $bindable(),
		dropBefore = $bindable(),
		programDrag = $bindable(),
		dragCell = $bindable(),
		icons,
		byKey,
		places,
		dragCells,
		colW,
		rowH,
		iconPx,
		left,
		open,
		dropOn,
		endIconDrag,
	}: {
		menu: DeskMenu | null;
		renaming: string | null;
		dragKey: string | null;
		dropBefore: string | null;
		programDrag: boolean;
		dragCell: Cell | null;
		icons: string[];
		byKey: Map<string, Program>;
		places: Map<string, Cell>;
		/** PLAN 66: the cells shown while dragging a program. */
		dragCells: Cell[];
		colW: number;
		rowH: number;
		iconPx: number;
		left: number;
		open: (key: string, event?: Event, href?: string) => void;
		dropOn: (event: DragEvent, target: string | null) => void;
		endIconDrag: () => void;
	} = $props();

	function renameFolder(key: string, name: string) {
		renaming = null;
		saveFolderName(key, name);
	}
</script>

<div class="icons" style:left="{left}px">
	{#if programDrag || dragKey}
		<div class="drag-grid" aria-hidden="true">
			{#each dragCells as { c, r } (c + ':' + r)}
				<span
					class="cell"
					class:target={!dropBefore && dragCell?.c === c && dragCell?.r === r}
					style:left="{c * colW}px"
					style:top="{r * rowH}px"
					style:width="{colW - 4}px"
					style:height="{rowH - 4}px"
				></span>
			{/each}
		</div>
	{/if}
	{#each icons as key (key)}
		{@const p = byKey.get(key)}
		{@const folder = key.startsWith('folder:') ? folderOf(key) : undefined}
		{#if p || folder}
			<button
				type="button"
				class="ico"
				data-key={key}
				class:drop={dropBefore === key && dragKey !== key}
				class:lifted={dragKey === key}
				style:left="{(places.get(key)?.c ?? 0) * colW}px"
				style:top="{(places.get(key)?.r ?? 0) * rowH}px"
				style:width="{colW - 4}px"
				draggable="true"
				ondragstart={(e) => {
					dragKey = key;
					e.dataTransfer?.setData('text/x-titanos-program', key);
					// The icon at its full size under the pointer (the browser shows it see-through).
					const el = e.currentTarget as HTMLElement;
					const box = el.getBoundingClientRect();
					e.dataTransfer?.setDragImage(el, e.clientX - box.left, e.clientY - box.top);
				}}
				ondragend={endIconDrag}
				ondragover={(e) => {
					e.preventDefault();
					e.stopPropagation();
					programDrag = true;
					dropBefore = dragKey === key ? null : key;
					dragCell = places.get(key) ?? null;
				}}
				ondrop={(e) => {
					e.stopPropagation();
					dropOn(e, key);
				}}
				ondblclick={(e) => open(key, e)}
				onclick={(e) => {
					// One click opens, like the menu (a double click does the same).
					if (e.detail === 1) open(key, e);
				}}
				oncontextmenu={(e) => {
					e.preventDefault();
					e.stopPropagation();
					menu = { ...menuAt(e), kind: 'icon', key };
				}}
				title={p?.badge?.title}
			>
				<span class="ico-wrap">
					{#if folder}
						<span class="folder-ico" style:width="var(--icon)" style:height="var(--icon)">
							{#each folder.apps.slice(0, 4) as app (app)}
								{@const fp = byKey.get(app)}
								{#if fp}<DesktopIcon
										glyph={fp.glyph}
										gradient={fp.gradient}
										image={fp.image}
										name={fp.label}
										size={Math.round(iconPx * 0.36)}
									/>{/if}
							{/each}
						</span>
					{:else if p}
						<DesktopIcon
							glyph={p.glyph}
							gradient={p.gradient}
							image={p.image}
							name={p.label}
							size={iconPx}
						/>
						{#if p.badge?.count}<span class="badge count" aria-label={p.badge.title}
								>{p.badge.count}</span
							>
						{:else if p.badge?.warn}<span class="badge warn" aria-label={p.badge.title}>!</span>
						{:else if p.badge?.dot}<span class="badge dot {p.badge.dot}" aria-label={p.badge.title}
							></span>{/if}
					{/if}
				</span>
				{#if renaming === key && folder}
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="rename"
						value={folder.name}
						autofocus
						onclick={(e) => e.stopPropagation()}
						onblur={(e) => renameFolder(key, e.currentTarget.value)}
						onkeydown={(e) => e.key === 'Enter' && renameFolder(key, e.currentTarget.value)}
						aria-label={t('desktop.folder.rename')}
					/>
				{:else}
					<span class="label" data-cut="excerpt">{folder?.name ?? p?.label}</span>
				{/if}
			</button>
		{/if}
	{/each}
</div>

<style>
	.icons {
		position: absolute;
		top: 8px;
		right: 0;
		bottom: 0;
		pointer-events: none;
	}
	.icons > .ico {
		position: absolute;
		pointer-events: auto;
		transition:
			left 0.18s ease,
			top 0.18s ease;
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
	/* Light themes: dark names with a light halo, readable on light wallpapers. */
	:global(.light) .ico:not(.small) {
		color: var(--text);
		text-shadow:
			0 0 3px rgb(255 255 255 / 0.9),
			0 1px 2px rgb(255 255 255 / 0.8);
	}
	.ico.drop {
		box-shadow:
			inset 0 0 0 2px var(--accent),
			0 0 18px color-mix(in srgb, var(--accent) 45%, transparent);
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	/* PLAN 66: the dragged icon stays faint in its old place. */
	.ico.lifted {
		opacity: 0.35;
	}
	.drag-grid {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}
	.drag-grid .cell {
		position: absolute;
		border: 1px dashed color-mix(in srgb, #fff 26%, transparent);
		border-radius: 12px;
		background: color-mix(in srgb, #fff 4%, transparent);
	}
	:global(.light) .drag-grid .cell {
		border-color: color-mix(in srgb, var(--text) 30%, transparent);
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	.drag-grid .cell.target {
		border: 2px solid var(--accent);
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		box-shadow: 0 0 22px color-mix(in srgb, var(--accent) 45%, transparent);
	}
	@media (prefers-reduced-motion: reduce) {
		.icons > .ico {
			transition: none;
		}
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
	.folder-ico {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-1);
		padding: var(--space-2);
		border-radius: 23%;
		background: color-mix(in srgb, #fff 20%, transparent);
		backdrop-filter: blur(10px);
		place-items: center;
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
	.badge.warn {
		background: #f0a91a;
		color: #2b2100;
	}
	.badge.dot {
		top: auto;
		bottom: -2px;
		right: -2px;
		min-width: 14px;
		width: 14px;
		height: 14px;
		padding: 0;
		background: #9aa7b6;
	}
	.badge.dot.on {
		background: #4fd67a;
	}
	.rename {
		width: 100%;
		font: inherit;
		font-size: var(--text-sm);
		border: 1px solid var(--accent);
		border-radius: 6px;
		background: var(--surface);
		color: var(--text);
		text-align: center;
	}
</style>
