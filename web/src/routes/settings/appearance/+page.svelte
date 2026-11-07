<script lang="ts">
	// Settings › Appearance (PLAN 64): one simple page – theme, accent and five
	// sliders that count everywhere (desktop, every window, the phone; not the
	// KI-Agent, which keeps its own look). Every change shows at once in all
	// windows and is saved to the account a moment later.
	import { onMount, untrack } from 'svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { appearance, useAppearance } from '#lib/theme/appearance.svelte.ts';
	import {
		ACCENT_IDS,
		ACCENTS,
		defaultAppearance,
		EFFECT_IDS,
		globalLook,
		LIMITS,
		resolvePalette,
		sanitizeAppearance,
		textContrast,
		withCorners,
		type Appearance,
		type CustomColors,
		type ThemeId,
	} from '#lib/theme/themes.ts';
	import { session } from '#lib/session.svelte.ts';
	import LoginAppearanceEditor from '#lib/login/LoginAppearanceEditor.svelte';
	import AppIconEditor from '#lib/components/AppIconEditor.svelte';
	import DesktopDialogs from '#lib/desktop/DesktopDialogs.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { sanitizeDesktop } from '#lib/desktop/prefs.ts';
	import { loadDesktop } from '#lib/desktop/wm.svelte.ts';
	import { wallpaperCss } from '#lib/desktop/wallpapers.ts';
	import Tabs from '#lib/components/Tabs.svelte';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import DockSettings from './DockSettings.svelte';

	const CUSTOM_FIELDS: (keyof CustomColors)[] = ['bg', 'surface', 'header', 'text', 'border'];

	// PLAN 67: tabs like Systemsteuerung › Netzwerk; the dock and the login
	// page are tabs too. Every tab stays in the page (hidden), so the unsaved
	// app symbol and login look keep their state while another tab shows.
	type Tab = 'colors' | 'glass' | 'background' | 'dock' | 'login';
	const TABS: Tab[] = ['colors', 'glass', 'background', 'dock', 'login'];
	const tabFrom = (value: string | null): Tab =>
		TABS.includes(value as Tab) ? (value as Tab) : 'colors';
	let tab = $state<Tab>(tabFrom(page.url.searchParams.get('tab')));
	function chooseTab(next: Tab) {
		tab = next;
		replaceState(
			next === 'colors' ? '/settings/appearance' : `/settings/appearance?tab=${next}`,
			{},
		);
	}
	const DARK_THEMES = ['graphit', 'schwarz', 'mitternacht', 'eigen'] as const;

	let draft = $state<Appearance>($state.snapshot(appearance.current));
	let state_ = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
	let error = $state('');
	let themeFile: HTMLInputElement;

	// The app symbol and the login page (owners/admins) keep their "Apply".
	let iconEditor = $state<AppIconEditor>();
	let loginEditor = $state<LoginAppearanceEditor>();
	let iconDirty = $state(false);
	let loginDirty = $state(false);
	let extraBusy = $state(false);
	let extraMessage = $state<{ text: string; warn: boolean } | null>(null);
	const canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	const tabs = $derived(
		TABS.filter((id) => id !== 'login' || canAdminister).map((id) => ({
			id,
			label: t(`look.tab.${id}`),
		})),
	);
	$effect(() => {
		if (tab === 'login' && session.data?.user && !canAdminister) tab = 'colors';
	});

	const palette = $derived(resolvePalette(draft, appearance.systemLight));
	const ratio = $derived(textContrast(palette));
	/** "hell", "dunkel" or "auto" as the first choice; the dark themes are variants. */
	const mode = $derived<'light' | 'dark' | 'auto'>(
		draft.theme === 'auto' ? 'auto' : draft.theme === 'hell' ? 'light' : 'dark',
	);
	let lastDark = $state<ThemeId>(
		(DARK_THEMES as readonly string[]).includes(appearance.current.theme)
			? appearance.current.theme
			: 'graphit',
	);
	const modes = $derived<{ value: 'light' | 'dark' | 'auto'; label: string }[]>([
		{ value: 'light', label: t('look.mode.light') },
		{ value: 'dark', label: t('look.mode.dark') },
		{ value: 'auto', label: t('look.mode.auto') },
	]);
	function setMode(value: 'light' | 'dark' | 'auto') {
		draft.theme = value === 'light' ? 'hell' : value === 'auto' ? 'auto' : lastDark;
	}
	function setDark(id: ThemeId) {
		lastDark = id;
		draft.theme = id;
	}
	function look(id: ThemeId) {
		return resolvePalette({ ...draft, theme: id }, appearance.systemLight);
	}

	// Every change: shown in all windows at once, saved a moment after the last one.
	let timer: ReturnType<typeof setTimeout> | undefined;
	let first = true;
	$effect(() => {
		const value = globalLook($state.snapshot(draft) as Appearance);
		JSON.stringify(value);
		if (first) {
			first = false;
			return;
		}
		// Only the draft is followed; using it writes the shared appearance.
		untrack(() => useAppearance(value));
		state_ = 'saving';
		clearTimeout(timer);
		timer = setTimeout(async () => {
			try {
				await api.put('/preferences/appearance', value);
				state_ = 'saved';
				error = '';
			} catch (err) {
				state_ = 'error';
				error = apiErrorText(err);
			}
		}, 700);
	});
	onMount(() => () => clearTimeout(timer));

	function reset() {
		// The uploaded image stays on the NAS; only the settings go back.
		draft = { ...defaultAppearance(), bgVersion: draft.bgVersion, nav: draft.nav };
	}

	function exportTheme() {
		const { bgVersion: _image, ...theme } = $state.snapshot(draft);
		const blob = new Blob([JSON.stringify({ titanosTheme: 1, ...theme }, null, '\t') + '\n'], {
			type: 'application/json',
		});
		const link = document.createElement('a');
		link.href = URL.createObjectURL(blob);
		link.download = 'titanos-theme.json';
		link.click();
		URL.revokeObjectURL(link.href);
	}

	async function importTheme() {
		const file = themeFile.files?.[0];
		themeFile.value = '';
		if (!file) return;
		try {
			const data = JSON.parse(await file.text());
			if (typeof data !== 'object' || data === null || !('titanosTheme' in data))
				throw new Error('not a theme');
			draft = { ...sanitizeAppearance(data), bgVersion: draft.bgVersion, nav: draft.nav };
		} catch {
			state_ = 'error';
			error = t('appearance.importFailed');
		}
	}

	// The wallpaper belongs to the desktop: its own dialog there; on the phone here.
	let wallpaperDialog = $state(false);
	const wallpaperPreview = $derived.by(() => {
		const wp = sanitizeDesktop(session.desktop).wallpaper;
		return wp.source === 'custom'
			? `url("/api/v1/background?v=${wp.version}") center / cover`
			: `${wallpaperCss(wp.id, !palette.dark)} center / cover`;
	});
	function chooseWallpaper() {
		if (inWindow)
			window.parent.postMessage(
				{ type: 'titanos-desk-dialog', dialog: 'wallpaper' },
				location.origin,
			);
		else {
			loadDesktop();
			wallpaperDialog = true;
		}
	}

	async function applyExtras() {
		extraBusy = true;
		extraMessage = null;
		const failures: string[] = [];
		const steps: [string, boolean, () => Promise<unknown>][] = [
			[t('appIcon.title'), iconDirty, async () => iconEditor?.save()],
			[t('loginLook.title'), loginDirty, async () => loginEditor?.save()],
		];
		for (const [area, changed, save] of steps) {
			if (!changed) continue;
			try {
				await save();
			} catch (err) {
				failures.push(t('appearance.saveFailed', { area, error: apiErrorText(err) }));
			}
		}
		extraMessage = failures.length
			? { text: failures.join(' · '), warn: true }
			: { text: t('appearance.saved'), warn: false };
		extraBusy = false;
	}
	function discardExtras() {
		iconEditor?.discard();
		loginEditor?.discard();
		extraMessage = null;
	}
</script>

{#snippet slider(
	id: string,
	label: string,
	hint: string,
	value: number,
	limit: { min: number; max: number; step: number },
	unit: string,
	set: (v: number) => void,
)}
	<div class="set-row slider">
		<span class="slider-top">
			<span class="set-label">
				<label for={id}>{label}</label>
				<span class="set-hint">{hint}</span>
			</span>
			<output for={id} class="value">{value} {unit}</output>
		</span>
		<input
			{id}
			type="range"
			min={limit.min}
			max={limit.max}
			step={limit.step}
			{value}
			oninput={(e) => set(Number(e.currentTarget.value))}
		/>
	</div>
{/snippet}

{#snippet thumb(kind: 'light' | 'dark' | 'auto')}
	{@const light = look('hell')}
	{@const dark = look(lastDark)}
	<span class="thumb-mini {kind}" aria-hidden="true">
		{#each kind === 'auto' ? [light, dark] : [kind === 'light' ? light : dark] as c, i (i)}
			<span class="half" style:background={c.bg}>
				<span class="bar" style:background={c.surface}></span>
				<span class="pill"></span>
				<span class="block" style:background={c.surface} style:border-color={c.border}></span>
			</span>
		{/each}
	</span>
{/snippet}

<PageHead title={t('appearance.title')}>
	<p>{t('look.intro')}</p>
</PageHead>

<div class="tab-row">
	<Tabs label={t('look.tabs')} {tabs} value={tab} onchange={chooseTab} />
	<p class="status" role="status" aria-live="polite" data-state={state_}>
		{#if state_ === 'saving'}{t('ui.saving')}{:else if state_ === 'saved'}{t(
				'appearance.saved',
			)}{:else if state_ === 'error'}<span class="warn">{error}</span>{/if}
	</p>
</div>

<!-- Farben: mode, variant, accent and the five sliders (approved draft 7.11.2026). -->
<div class="tab-panel" hidden={tab !== 'colors'} data-tab-panel="colors">
	<section class="tile ui-card" aria-labelledby="h-colors">
		<div class="card-head"><h2 id="h-colors">{t('look.colors')}</h2></div>
		<div class="set-row">
			<span class="set-label">{t('look.mode')}</span>
			<div class="modes" role="radiogroup" aria-label={t('look.mode')}>
				{#each modes as m (m.value)}
					<button
						type="button"
						role="radio"
						class="mode"
						aria-checked={mode === m.value}
						onclick={() => setMode(m.value)}
					>
						{@render thumb(m.value)}
						<span class="mode-name">{m.label}</span>
					</button>
				{/each}
			</div>
		</div>
		{#if mode === 'dark'}
			<div class="set-row">
				<span class="set-label">{t('look.variant')}</span>
				<div class="variants" role="radiogroup" aria-label={t('look.variant')}>
					{#each DARK_THEMES as id (id)}
						{@const colors = look(id)}
						<button
							type="button"
							role="radio"
							class="variant"
							aria-checked={draft.theme === id}
							onclick={() => setDark(id)}
						>
							<span
								class="swatch"
								aria-hidden="true"
								style:background={colors.bg}
								style:border-color={colors.border}
							></span>{t(`theme.${id}`)}{#if id === 'eigen'}&nbsp;…{/if}
						</button>
					{/each}
				</div>
			</div>
			{#if draft.theme === 'eigen'}
				<div class="set-row">
					<span class="set-label">{t('look.customColors')}</span>
					<div class="set-control">
						<div class="custom">
							{#each CUSTOM_FIELDS as field (field)}
								<label class="color">
									<input type="color" bind:value={draft.custom[field]} />
									<span>{t(`custom.${field}`)}</span>
								</label>
							{/each}
						</div>
						{#if ratio < 4.5}
							<p class="notice-warn">
								{t('appearance.contrastWarning', {
									ratio:
										new Intl.NumberFormat(i18n.locale, { maximumFractionDigits: 1 }).format(ratio) +
										':1',
								})}
							</p>
						{/if}
					</div>
				</div>
			{/if}
		{/if}
		<div class="set-row">
			<span class="set-label">{t('appearance.accent')}</span>
			<div class="accents" role="radiogroup" aria-label={t('appearance.accent')}>
				{#each ACCENT_IDS as id (id)}
					<button
						type="button"
						role="radio"
						class="pickdot"
						aria-label={t(`accent.${id}`)}
						title={t(`accent.${id}`)}
						aria-checked={draft.accent === id}
						style:background={ACCENTS[id].hex}
						onclick={() => (draft.accent = id)}
					></button>
				{/each}
				<label class="own-accent" class:picked={draft.accent === 'custom'}>
					<input
						type="color"
						aria-label={t('appearance.customAccent')}
						bind:value={draft.customAccent}
						oninput={() => (draft.accent = 'custom')}
					/>
					<span>{t('look.accentCustom')}</span>
				</label>
			</div>
		</div>
	</section>

	<section class="tile ui-card" aria-labelledby="h-sliders">
		<div class="card-head"><h2 id="h-sliders">{t('look.sliders')}</h2></div>
		<div class="set-grid">
			{@render slider(
				'look-glass',
				t('appearance.transparency'),
				t('look.glassHint'),
				draft.glass,
				LIMITS.glass,
				'%',
				(v) => (draft.glass = v),
			)}
			{@render slider(
				'look-blur',
				t('look.blur'),
				t('look.blurHint'),
				draft.blur,
				LIMITS.blur,
				'px',
				(v) => (draft.blur = v),
			)}
			{@render slider(
				'look-corners',
				t('look.corners'),
				t('look.cornersHint'),
				draft.rTile,
				LIMITS.rTile,
				'px',
				(v) => (draft = withCorners(draft, v)),
			)}
			{@render slider(
				'look-font',
				t('appearance.fontSize'),
				t('look.fontHint'),
				draft.font,
				LIMITS.font,
				'%',
				(v) => (draft.font = v),
			)}
			{@render slider(
				'look-saturation',
				t('look.saturation'),
				t('look.saturationHint'),
				draft.saturation,
				LIMITS.saturation,
				'%',
				(v) => (draft.saturation = v),
			)}
		</div>
		<div class="card-foot">
			<span class="grow">{t('look.fileHint')}</span>
			<button type="button" class="btn" onclick={exportTheme}>{t('appearance.export')}</button>
			<button type="button" class="btn" onclick={() => themeFile.click()}
				>{t('appearance.import')}</button
			>
			<button type="button" class="btn plain" onclick={reset}>{t('appearance.reset')}</button>
			<input
				bind:this={themeFile}
				type="file"
				accept="application/json,.json"
				hidden
				onchange={importTheme}
			/>
		</div>
	</section>
</div>

<!-- Glas & Schrift: the finer settings (spacing, edges, glow, motion). -->
<div class="tab-panel" hidden={tab !== 'glass'} data-tab-panel="glass">
	<section class="tile ui-card" aria-labelledby="h-details">
		<div class="card-head">
			<h2 id="h-details">{t('look.details')}</h2>
			<span class="card-meta">{t('look.detailsHint')}</span>
		</div>
		<div class="set-row">
			<span class="set-label"
				>{t('appearance.density')}<span class="set-hint">{t('look.densityHint')}</span></span
			>
			<span class="set-control">
				<Segments
					label={t('appearance.density')}
					options={[
						{ value: 'normal', label: t('density.normal') },
						{ value: 'kompakt', label: t('density.kompakt') },
					]}
					value={draft.density}
					onchange={(v) => (draft.density = v)}
				/>
			</span>
		</div>
		<label class="set-row">
			<span class="set-label"
				>{t('look.motion')}<span class="set-hint">{t('look.motionHint')}</span></span
			>
			<span class="set-control end"
				><input type="checkbox" role="switch" class="switch" bind:checked={draft.motion} /></span
			>
		</label>
		<div class="set-grid">
			{@render slider(
				'look-shine',
				t('appearance.shine'),
				t('look.shineHint'),
				draft.shine,
				LIMITS.shine,
				'%',
				(v) => (draft.shine = v),
			)}
			{@render slider(
				'look-glow',
				t('dashboard.visual.glow'),
				t('look.glowHint'),
				draft.glow,
				LIMITS.glow,
				'%',
				(v) => (draft.glow = v),
			)}
			{@render slider(
				'look-dim',
				t('appearance.floating.dim'),
				t('look.dimHint'),
				draft.dialogs.dim,
				LIMITS.dialogDim,
				'%',
				(v) => (draft.dialogs.dim = v),
			)}
		</div>
	</section>

	<section class="tile ui-card" aria-labelledby="h-fx">
		<div class="card-head"><h2 id="h-fx">{t('look.effects')}</h2></div>
		<label class="set-row">
			<span class="set-label"
				>{t('appearance.fx.enabled')}<span class="set-hint">{t('look.fxHint')}</span></span
			>
			<span class="set-control end"
				><input
					type="checkbox"
					role="switch"
					class="switch"
					bind:checked={draft.fx.enabled}
				/></span
			>
		</label>
		{#if draft.fx.enabled}
			{#each EFFECT_IDS as id (id)}
				<label class="set-row">
					<span class="set-label"
						>{t(`appearance.fx.${id}`)}<span class="set-hint">{t(`appearance.fx.${id}Desc`)}</span
						></span
					>
					<span class="set-control end"
						><input
							type="checkbox"
							class="switch"
							role="switch"
							bind:checked={draft.fx[id]}
						/></span
					>
				</label>
			{/each}
		{/if}
		<p class="card-note">{t('appearance.fx.hint')}</p>
	</section>
</div>

<!-- Hintergrund: the desktop's picture; the app symbol of the NAS (owners/admins). -->
<div class="tab-panel" hidden={tab !== 'background'} data-tab-panel="background">
	<section class="tile ui-card" aria-labelledby="h-desk">
		<div class="card-head"><h2 id="h-desk">{t('desktop.wallpaper.title')}</h2></div>
		<div class="set-row">
			<span class="set-label"
				>{t('look.desktop')}<span class="set-hint">{t('look.wallpaperHint')}</span></span
			>
			<span class="set-control">
				<span class="wall" style:background={wallpaperPreview} aria-hidden="true"></span>
				<button type="button" class="btn" onclick={chooseWallpaper}>{t('look.wallpaper')}</button>
			</span>
		</div>
	</section>
	{#if canAdminister}
		<AppIconEditor bind:this={iconEditor} bind:dirty={iconDirty} />
	{/if}
</div>

<!-- Dock: the former page /settings/appearance/dock. -->
<div class="tab-panel" hidden={tab !== 'dock'} data-tab-panel="dock">
	<DockSettings />
</div>

<!-- Anmeldeseite (owners/admins): the account picker and the login look. -->
{#if canAdminister}
	<div class="tab-panel" hidden={tab !== 'login'} data-tab-panel="login">
		<LoginAppearanceEditor bind:this={loginEditor} bind:dirty={loginDirty} />
	</div>
	{#if iconDirty || loginDirty || extraMessage}
		<div class="save-bar" class:pinned={iconDirty || loginDirty}>
			<span class="state" class:warn={extraMessage?.warn} role="status"
				>{#if extraBusy}{t('ui.saving')}{:else if iconDirty || loginDirty}{t(
						'appearance.unsaved',
					)}{:else if extraMessage}{extraMessage.text}{/if}</span
			>
			{#if extraMessage?.warn && (iconDirty || loginDirty)}<p class="notice-error" role="alert">
					{extraMessage.text}
				</p>{/if}
			{#if iconDirty || loginDirty}<div class="row-actions">
					<button type="button" class="btn" disabled={extraBusy} onclick={discardExtras}
						>{t('appearance.discard')}</button
					><button type="button" class="btn btn-primary" disabled={extraBusy} onclick={applyExtras}
						>{t('appearance.apply')}</button
					>
				</div>{/if}
		</div>
	{/if}
{/if}

{#if wallpaperDialog}
	<DesktopDialogs
		kind="wallpaper"
		kinds={[]}
		onaddwidget={() => {}}
		onclose={() => (wallpaperDialog = false)}
	/>
{/if}

<style>
	.tab-row {
		position: relative;
		display: flex;
		align-items: flex-end;
		gap: 12px;
		min-width: 0;
	}
	.tab-row :global(.tabs) {
		flex: 1;
		min-width: 0;
	}
	.status {
		position: absolute;
		right: 0;
		bottom: 12px;
		margin: 0;
		max-width: 50%;
		font-size: var(--text-sm);
		color: var(--muted);
		text-align: right;
		pointer-events: none;
	}
	.status[data-state='error'] {
		pointer-events: auto;
	}
	.warn {
		color: var(--warn-text, var(--text));
	}
	.tab-panel {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
		min-width: 0;
	}
	.tab-panel[hidden] {
		display: none;
	}
	/* Modus: three small previews. */
	.modes {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
	}
	.mode {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		padding: 0;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.mode[aria-checked='true'] {
		color: var(--text);
		font-weight: 600;
	}
	.thumb-mini {
		display: flex;
		width: 88px;
		height: 54px;
		padding: 3px;
		border: 2px solid var(--border);
		border-radius: 10px;
		overflow: hidden;
	}
	.mode[aria-checked='true'] .thumb-mini {
		border-color: var(--accent);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 25%, transparent);
	}
	.half {
		position: relative;
		flex: 1;
		border-radius: 6px;
		overflow: hidden;
	}
	.auto .half:first-child {
		border-radius: 6px 0 0 6px;
	}
	.auto .half:last-child {
		border-radius: 0 6px 6px 0;
	}
	.half .bar {
		position: absolute;
		left: 8px;
		top: 9px;
		width: 34%;
		height: 5px;
		border-radius: 3px;
	}
	.half .pill {
		position: absolute;
		background: var(--accent);
		left: 8px;
		top: 20px;
		width: 22%;
		height: 5px;
		border-radius: 3px;
	}
	.half .block {
		position: absolute;
		right: 6px;
		top: 7px;
		bottom: 7px;
		width: 40%;
		border: 1px solid;
		border-radius: 5px;
	}
	.auto .half .block {
		width: 30%;
	}
	.variants,
	.accents {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.variant {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		min-height: 32px;
		padding: 0 12px 0 8px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.variant[aria-checked='true'] {
		border-color: var(--accent);
		box-shadow: inset 0 0 0 1px var(--accent);
	}
	.swatch {
		width: 16px;
		height: 16px;
		border: 1px solid;
		border-radius: 4px;
	}
	.custom {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 8px;
		width: 100%;
	}
	.color {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 4px 8px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
		cursor: pointer;
	}
	input[type='color'] {
		width: 26px;
		height: 26px;
		padding: 0;
		border: 0;
		background: transparent;
		cursor: pointer;
	}
	.pickdot {
		width: 26px;
		height: 26px;
		padding: 0;
		border: 2px solid transparent;
		border-radius: 50%;
		cursor: pointer;
		box-shadow: 0 0 0 1px var(--border);
	}
	.pickdot[aria-checked='true'] {
		box-shadow:
			0 0 0 2px var(--bg),
			0 0 0 4px var(--text);
	}
	.own-accent {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		margin-left: 6px;
		color: var(--muted);
		font-size: var(--text-base);
		cursor: pointer;
	}
	.own-accent.picked {
		color: var(--text);
		font-weight: 600;
	}
	.own-accent input {
		width: 22px;
		height: 22px;
	}
	/* Sliders: name and value on one line, the hint below, the slider under it. */
	.slider {
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 8px;
	}
	.slider-top {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
	}
	.value {
		flex: none;
		color: var(--muted);
		font-size: var(--text-base);
		font-variant-numeric: tabular-nums;
	}
	.slider input {
		width: 100%;
		margin: 0;
	}
	.plain {
		border-color: transparent;
	}
	.card-note {
		margin: 0;
		padding: 10px 16px 14px;
		border-top: 1px solid var(--panel-border, var(--border));
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.wall {
		width: 88px;
		height: 52px;
		border-radius: 8px;
		border: 1px solid var(--border);
		filter: var(--wallpaper-filter, none);
	}
	/* The bar of the app symbol and the login look: unsaved changes. */
	.save-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
		padding: 10px 16px;
		border: 1px solid var(--panel-border, var(--border));
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--panel-bg, var(--surface));
	}
	.save-bar.pinned {
		position: sticky;
		bottom: 12px;
		z-index: 15;
		box-shadow: 0 8px 24px rgb(0 0 0 / 0.25);
	}
	.save-bar .state {
		flex: 1 1 12rem;
		font-size: var(--text-base);
	}
	.save-bar .state.warn {
		color: var(--warn-text);
	}
	.save-bar .notice-error {
		flex-basis: 100%;
		margin: 0;
	}
	.save-bar .row-actions {
		display: flex;
		gap: 8px;
	}
	@media (max-width: 639px) {
		.status {
			position: static;
			max-width: none;
		}
		.tab-row {
			flex-direction: column;
			align-items: stretch;
			gap: 4px;
		}
	}
</style>
