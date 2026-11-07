<script lang="ts">
	// PLAN 61: the small dialogs of the desktop – wallpaper, widget gallery,
	// saved window arrangements and the desktop's own settings.
	import Dialog from '../components/Dialog.svelte';
	import { api, ApiError } from '../api.ts';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';
	import { appearance } from '../theme/appearance.svelte.ts';
	import { THEMES } from '../theme/themes.ts';
	import { WALLPAPERS, type WidgetKind } from './prefs.ts';
	import { wallpaperCss } from './wallpapers.ts';
	import { desktop, savePrefs, wm, openWindow, saveWindows } from './wm.svelte.ts';

	let {
		kind,
		onclose,
		onaddwidget,
		kinds,
	}: {
		kind: 'wallpaper' | 'settings' | 'gallery' | 'layout';
		onclose: () => void;
		onaddwidget: (kind: WidgetKind, size: 's' | 'm' | 'l') => void;
		kinds: WidgetKind[];
	} = $props();

	const light = $derived(THEMES[appearance.current.theme as keyof typeof THEMES]?.dark === false);
	let error = $state('');
	let busy = $state(false);

	async function upload(event: Event) {
		const file = (event.currentTarget as HTMLInputElement).files?.[0];
		if (!file) return;
		error = '';
		busy = true;
		try {
			const res = await fetch('/api/v1/background', {
				method: 'PUT',
				body: file,
				headers: { 'Content-Type': file.type || 'application/octet-stream' },
				credentials: 'same-origin',
			});
			if (!res.ok)
				throw new ApiError(
					res.status,
					'upload',
					(await res.json().catch(() => null))?.error?.message ?? res.statusText,
				);
			savePrefs(
				(p) => (p.wallpaper = { source: 'custom', version: Date.now(), live: p.wallpaper.live }),
			);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
		busy = false;
	}

	let layoutName = $state('');
	function saveLayout() {
		const name = layoutName.trim() || t('desktop.layout.defaultName');
		const windows = wm.windows
			.filter((w) => !w.minimized && !w.app.startsWith('docker:'))
			.map((w) => ({ app: w.app, href: w.href, x: w.x, y: w.y, w: w.w, h: w.h }));
		savePrefs(
			(p) =>
				(p.layouts = [
					...p.layouts.slice(-9),
					{ id: 'l' + Date.now().toString(36), name, windows },
				]),
		);
		layoutName = '';
	}
	function restoreLayout(id: string) {
		const layout = desktop.prefs.layouts.find((l) => l.id === id);
		if (!layout) return;
		for (const lw of layout.windows) {
			const winId = openWindow(
				lw.app,
				lw.href,
				t(`desktop.app.${lw.app.slice(4)}` as MessageKey) ?? lw.app,
			);
			const win = wm.windows.find((w) => w.id === winId);
			if (win)
				Object.assign(win, { x: lw.x, y: lw.y, w: lw.w, h: lw.h, snap: null, minimized: false });
		}
		saveWindows();
		onclose();
	}
	void api;

	const WIDGET_TEXT: Record<WidgetKind, [MessageKey, MessageKey]> = {
		status: ['desktop.widget.status', 'desktop.gallery.status'],
		cpuram: ['desktop.widget.cpuram', 'desktop.gallery.cpuram'],
		network: ['desktop.widget.network', 'desktop.gallery.network'],
		volumes: ['desktop.widget.volumes', 'desktop.gallery.volumes'],
		docker: ['desktop.widget.docker', 'desktop.gallery.docker'],
		vms: ['desktop.widget.vms', 'desktop.gallery.vms'],
		backups: ['desktop.widget.backups', 'desktop.gallery.backups'],
		temperatures: ['desktop.widget.temperatures', 'desktop.gallery.temperatures'],
		clock: ['desktop.widget.clock', 'desktop.gallery.clock'],
		homeassistant: ['desktop.widget.homeassistant', 'desktop.gallery.homeassistant'],
		tasks: ['desktop.widget.tasks', 'desktop.gallery.tasks'],
	};
	const LOCK = [0, 5, 10, 15, 30, 60];
	const LOCK_PHONE = [0, 5, 15, 30, 60, 240, 1440];
</script>

{#if kind === 'wallpaper'}
	<Dialog title={t('desktop.wallpaper.title')} wide {onclose}>
		<div class="papers">
			{#each WALLPAPERS as id (id)}
				{@const on =
					desktop.prefs.wallpaper.source === 'builtin' && desktop.prefs.wallpaper.id === id}
				<button
					type="button"
					class="paper"
					class:on
					aria-pressed={on}
					style:background="{wallpaperCss(id, light)} center / cover"
					onclick={() =>
						savePrefs((p) => (p.wallpaper = { source: 'builtin', id, live: p.wallpaper.live }))}
				>
					<span>{t(`desktop.wallpaper.${id}` as MessageKey)}</span>
				</button>
			{/each}
		</div>
		<div class="row">
			<label class="btn">
				{t('desktop.wallpaper.upload')}
				<input
					type="file"
					accept="image/jpeg,image/png,image/webp"
					hidden
					onchange={upload}
					disabled={busy}
				/>
			</label>
			<span class="muted small">{t('desktop.wallpaper.uploadHint')}</span>
		</div>
		{#if error}<p class="error" role="alert">{error}</p>{/if}
		<label class="check"
			><input
				type="checkbox"
				checked={desktop.prefs.wallpaper.live}
				onchange={(e) =>
					savePrefs((p) => (p.wallpaper = { ...p.wallpaper, live: e.currentTarget.checked }))}
			/>
			{t('desktop.wallpaper.live')}</label
		>
		<p class="muted small">{t('desktop.wallpaper.liveHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={onclose}
				>{t('desktop.widgets.done')}</button
			>
		</div>
	</Dialog>
{:else if kind === 'gallery'}
	<Dialog title={t('desktop.widgets.add')} wide {onclose}>
		<div class="gallery">
			{#each kinds as k (k)}
				{@const there = desktop.prefs.widgets.some((w) => w.kind === k)}
				<div class="g-item">
					<b
						>{t(WIDGET_TEXT[k][0])}{#if there}<span class="muted small">
								· {t('desktop.gallery.shown')}</span
							>{/if}</b
					>
					<span class="muted small">{t(WIDGET_TEXT[k][1])}</span>
					<span class="sizes">
						{#each ['s', 'm', 'l'] as const as size (size)}
							<button
								type="button"
								class="btn"
								onclick={() => onaddwidget(k, size)}
								aria-label={t('desktop.gallery.addSize', {
									size: size.toUpperCase(),
									name: t(WIDGET_TEXT[k][0]),
								})}>+ {size.toUpperCase()}</button
							>
						{/each}
					</span>
				</div>
			{/each}
		</div>
		<p class="muted small">{t('desktop.gallery.hint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={onclose}
				>{t('desktop.widgets.done')}</button
			>
		</div>
	</Dialog>
{:else if kind === 'layout'}
	<Dialog title={t('desktop.layout.title')} {onclose}>
		<p class="muted">{t('desktop.layout.text')}</p>
		<div class="row">
			<input
				class="input"
				bind:value={layoutName}
				placeholder={t('desktop.layout.namePlaceholder')}
				aria-label={t('desktop.layout.name')}
				maxlength="60"
			/>
			<button
				type="button"
				class="btn btn-primary"
				onclick={saveLayout}
				disabled={!wm.windows.some((w) => !w.minimized)}>{t('desktop.layout.save')}</button
			>
		</div>
		{#each desktop.prefs.layouts as l (l.id)}
			<div class="layout-row">
				<span
					><b>{l.name}</b>
					<span class="muted small">{t('desktop.layout.count', { count: l.windows.length })}</span
					></span
				>
				<span class="space"></span>
				<button type="button" class="btn" onclick={() => restoreLayout(l.id)}
					>{t('desktop.layout.restore')}</button
				>
				<button
					type="button"
					class="btn"
					onclick={() => savePrefs((p) => (p.layouts = p.layouts.filter((x) => x.id !== l.id)))}
					aria-label={t('desktop.layout.delete', { name: l.name })}>✕</button
				>
			</div>
		{:else}
			<p class="muted small">{t('desktop.layout.none')}</p>
		{/each}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		</div>
	</Dialog>
{:else}
	<Dialog title={t('desktop.settings.title')} {onclose}>
		<!-- PLAN 70: rows like every settings card (label and hint left, control right). -->
		<div class="rows ui-card">
			<div class="set-row">
				<label class="set-label" for="d-lock"
					>{t('desktop.settings.lock')}<span class="set-hint">{t('desktop.settings.lockHint')}</span
					></label
				>
				<span class="set-control end">
					<select
						id="d-lock"
						class="input"
						value={desktop.prefs.lockMinutes}
						onchange={(e) => savePrefs((p) => (p.lockMinutes = Number(e.currentTarget.value)))}
					>
						{#each LOCK as m (m)}<option value={m}
								>{m === 0
									? t('desktop.settings.never')
									: t('desktop.settings.minutes', { count: m })}</option
							>{/each}
					</select>
				</span>
			</div>
			<div class="set-row">
				<label class="set-label" for="d-lock-phone"
					>{t('desktop.settings.lockPhone')}<span class="set-hint"
						>{t('desktop.settings.lockPhoneHint')}</span
					></label
				>
				<span class="set-control end">
					<select
						id="d-lock-phone"
						class="input"
						value={desktop.prefs.lockMinutesPhone}
						onchange={(e) => savePrefs((p) => (p.lockMinutesPhone = Number(e.currentTarget.value)))}
					>
						{#each LOCK_PHONE as m (m)}<option value={m}
								>{m === 0
									? t('desktop.settings.never')
									: m >= 120
										? t('desktop.settings.hours', { count: m / 60 })
										: t('desktop.settings.minutes', { count: m })}</option
							>{/each}
					</select>
				</span>
			</div>
			<div class="set-row">
				<label class="set-label" for="d-size">{t('desktop.ctx.iconSize')}</label>
				<span class="set-control end">
					<select
						id="d-size"
						class="input"
						value={desktop.prefs.iconSize}
						onchange={(e) =>
							savePrefs((p) => (p.iconSize = e.currentTarget.value as 's' | 'm' | 'l'))}
					>
						<option value="s">{t('desktop.size.s')}</option><option value="m"
							>{t('desktop.size.m')}</option
						><option value="l">{t('desktop.size.l')}</option>
					</select>
				</span>
			</div>
			<div class="set-row">
				<label class="set-label" for="d-sort">{t('desktop.ctx.sort')}</label>
				<span class="set-control end">
					<select
						id="d-sort"
						class="input"
						value={desktop.prefs.sort}
						onchange={(e) => savePrefs((p) => (p.sort = e.currentTarget.value as typeof p.sort))}
					>
						<option value="kind">{t('desktop.sort.kind')}</option><option value="name"
							>{t('desktop.sort.name')}</option
						><option value="recent">{t('desktop.sort.recent')}</option><option value="free"
							>{t('desktop.sort.free')}</option
						>
					</select>
				</span>
			</div>
			<div class="set-row">
				<span class="set-label"
					>{t('desktop.settings.resetIcons')}<span class="set-hint"
						>{t('desktop.settings.resetIconsHint')}</span
					></span
				>
				<span class="set-control end">
					<button
						type="button"
						class="btn"
						onclick={() =>
							savePrefs((p) => ((p.icons = []), (p.customIcons = false), (p.folders = [])))}
						>{t('desktop.settings.reset')}</button
					>
				</span>
			</div>
		</div>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={onclose}
				>{t('desktop.widgets.done')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.papers {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 10px;
		margin-bottom: 14px;
	}
	.paper {
		height: 96px;
		border: 2px solid transparent;
		border-radius: 12px;
		display: flex;
		align-items: flex-end;
		padding: 8px;
		color: #fff;
		font: inherit;
		font-weight: 600;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.7);
		cursor: pointer;
	}
	.paper.on {
		border-color: var(--accent);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 35%, transparent);
	}
	.row,
	.layout-row {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		margin: 8px 0;
	}
	.space {
		flex: 1;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 12px;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.error {
		color: var(--danger-text, #ff9a9a);
	}
	.gallery {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 10px;
	}
	.g-item {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--surface);
	}
	.sizes {
		display: flex;
		gap: 6px;
		margin-top: 8px;
	}
	.rows {
		border: 1px solid var(--border);
		border-radius: var(--radius-card, 12px);
		overflow: hidden;
	}
	.rows .set-row + .set-row {
		border-top: 1px solid var(--panel-border, var(--border));
	}
	@media (max-width: 560px) {
		.rows .set-row {
			grid-template-columns: 1fr;
		}
		.rows .set-control.end {
			justify-content: flex-start;
		}
	}
</style>
