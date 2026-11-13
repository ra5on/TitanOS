<script lang="ts">
	// Settings › Appearance (PLAN 64): one simple page – theme, accent and five
	// sliders that count everywhere (desktop, every window, the phone; not the
	// KI-Agent, which keeps its own look). Every change shows at once in all
	// windows and is saved to the account a moment later.
	import { onMount, untrack } from 'svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Segments from '#lib/components/Segments.svelte';
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
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';
	import { barLink, connectBar, setBarLoad } from '#lib/desktop/bar-link.svelte.ts';

	// PLAN 79-10: CPU and RAM in the top bar of the desktop (per account).
	onMount(() => connectBar());

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
	/** PLAN 73/80-1: where "Transparenz" lies on the slider from see-through (0) to opaque (100). */
	const liquidOf = (glass: number) =>
		Math.max(0, Math.min(100, Math.round((80 - glass) / 0.75 / 5) * 5));
</script>

<!-- PLAN 78-1: a slider is a row with the slider and its value as control. -->
{#snippet slider(
	id: string,
	label: string,
	hint: string,
	value: number,
	limit: { min: number; max: number; step: number },
	unit: string,
	set: (v: number) => void,
)}
	<SettingRow title={label} desc={hint} for={id}>
		{#snippet control()}
			<span class="range">
				<input
					{id}
					type="range"
					min={limit.min}
					max={limit.max}
					step={limit.step}
					{value}
					oninput={(e) => set(Number(e.currentTarget.value))}
				/>
				<output for={id} class="value">{value} {unit}</output>
			</span>
		{/snippet}
	</SettingRow>
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

<!-- Farben und Schrift: mode, variant, accent, language and font size, the
     sliders (approved draft 7.11.2026; PLAN 78-1: groups are plain headings,
     every setting a row; PLAN 86: language and font size on this tab). -->
<div class="tab-panel" hidden={tab !== 'colors'} data-tab-panel="colors">
	<SettingGroup title={t('look.colors')} id="h-colors">
		<SettingRow title={t('look.mode')}>
			{#snippet control()}
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
			{/snippet}
		</SettingRow>
		{#if mode === 'dark'}
			<SettingRow title={t('look.variant')}>
				{#snippet control()}
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
				{/snippet}
			</SettingRow>
			{#if draft.theme === 'eigen'}
				<SettingRow title={t('look.customColors')}>
					{#snippet control()}
						<div class="custom">
							{#each CUSTOM_FIELDS as field (field)}
								<label class="color">
									<input type="color" bind:value={draft.custom[field]} />
									<span>{t(`custom.${field}`)}</span>
								</label>
							{/each}
						</div>
					{/snippet}
				</SettingRow>
				{#if ratio < 4.5}
					<p class="notice-warn">
						{t('appearance.contrastWarning', {
							ratio:
								new Intl.NumberFormat(i18n.locale, { maximumFractionDigits: 1 }).format(ratio) +
								':1',
						})}
					</p>
				{/if}
			{/if}
		{/if}
		<SettingRow title={t('appearance.accent')}>
			{#snippet control()}
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
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<!-- PLAN 86 (owner 13.11.2026): language and font size where one looks
	     for them – on the first tab, together, instead of under "Glas &
	     Schrift" and among the glass sliders. -->
	<SettingGroup title={t('look.langText')} id="h-lang">
		<SettingRow title={t('lang.label')} desc={t('look.languageHint')}>
			{#snippet control()}
				<LanguageSwitch />
			{/snippet}
		</SettingRow>
		{@render slider(
			'look-font',
			t('appearance.fontSize'),
			t('look.fontHint'),
			draft.font,
			LIMITS.font,
			'%',
			(v) => (draft.font = v),
		)}
	</SettingGroup>

	<SettingGroup title={t('look.sliders')} id="h-sliders">
		<!-- PLAN 73: one slider from see-through to opaque instead of
		     "Transparenz" and "Glaseffekt"; it sets both stored values (older
		     apps read them as before). PLAN 80-1: named "Deckkraft" since the
		     glass frame was dropped (10.11.2026). The live preview stands above
		     its row. -->
		<div class="liquid-preview" aria-hidden="true">
			<span
				class="pane"
				style:background={`color-mix(in srgb, var(--surface) ${100 - draft.glass}%, transparent)`}
				style:backdrop-filter={`blur(${draft.blur}px) saturate(1.4)`}
				><b>15:11</b><span>{t('look.liquidSample')}</span></span
			>
		</div>
		<SettingRow title={t('look.liquid')} desc={t('look.liquidHint')} for="look-liquid">
			{#snippet control()}
				<span class="range liquid-ends"
					><span>{t('look.liquidClear')}</span><input
						id="look-liquid"
						type="range"
						min="0"
						max="100"
						step="5"
						value={liquidOf(draft.glass)}
						aria-valuetext={t('look.liquidValue', { value: liquidOf(draft.glass) })}
						oninput={(e) => {
							const v = Number(e.currentTarget.value);
							draft.glass = Math.round((80 - 0.75 * v) / 5) * 5;
							draft.blur = Math.round((10 + 0.2 * v) / 2) * 2;
						}}
					/><span>{t('look.liquidTinted')}</span></span
				>
			{/snippet}
		</SettingRow>
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
			'look-saturation',
			t('look.saturation'),
			t('look.saturationHint'),
			draft.saturation,
			LIMITS.saturation,
			'%',
			(v) => (draft.saturation = v),
		)}
	</SettingGroup>

	<SettingGroup title={t('look.file')}>
		<SettingRow title={t('look.fileRow')} desc={t('look.fileHint')}>
			{#snippet control()}
				<button type="button" class="btn" onclick={exportTheme}>{t('appearance.export')}</button>
				<button type="button" class="btn" onclick={() => themeFile.click()}
					>{t('appearance.import')}</button
				>
				<input
					bind:this={themeFile}
					type="file"
					accept="application/json,.json"
					hidden
					onchange={importTheme}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow title={t('appearance.reset')}>
			{#snippet control()}
				<button type="button" class="btn" onclick={reset}>{t('desktop.dock.reset')}</button>
			{/snippet}
		</SettingRow>
	</SettingGroup>
</div>

<!-- Feinheiten und Effekte: the finer settings (spacing, edges, glow, motion). -->
<div class="tab-panel" hidden={tab !== 'glass'} data-tab-panel="glass">
	<SettingGroup title={t('look.details')} id="h-details" desc={t('look.detailsHint')}>
		<SettingRow title={t('appearance.density')} desc={t('look.densityHint')}>
			{#snippet control()}
				<Segments
					label={t('appearance.density')}
					options={[
						{ value: 'normal', label: t('density.normal') },
						{ value: 'kompakt', label: t('density.kompakt') },
					]}
					value={draft.density}
					onchange={(v) => (draft.density = v)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow title={t('look.motion')} desc={t('look.motionHint')} for="look-motion">
			{#snippet control()}
				<input
					id="look-motion"
					type="checkbox"
					role="switch"
					class="switch"
					bind:checked={draft.motion}
				/>
			{/snippet}
		</SettingRow>
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
	</SettingGroup>

	<SettingGroup title={t('look.effects')} id="h-fx">
		<SettingRow title={t('appearance.fx.enabled')} desc={t('look.fxHint')} for="look-fx">
			{#snippet control()}
				<input
					id="look-fx"
					type="checkbox"
					role="switch"
					class="switch"
					bind:checked={draft.fx.enabled}
				/>
			{/snippet}
		</SettingRow>
		{#if draft.fx.enabled}
			{#each EFFECT_IDS as id (id)}
				<SettingRow
					title={t(`appearance.fx.${id}`)}
					desc={t(`appearance.fx.${id}Desc`)}
					for="look-fx-{id}"
				>
					{#snippet control()}
						<input
							id="look-fx-{id}"
							type="checkbox"
							class="switch"
							role="switch"
							bind:checked={draft.fx[id]}
						/>
					{/snippet}
				</SettingRow>
			{/each}
		{/if}
		<p class="group-note">{t('appearance.fx.hint')}</p>
	</SettingGroup>
</div>

<!-- Hintergrund: the desktop's picture; the app symbol of the NAS (owners/admins). -->
<div class="tab-panel" hidden={tab !== 'background'} data-tab-panel="background">
	<SettingGroup title={t('desktop.wallpaper.title')} id="h-desk">
		<SettingRow title={t('look.desktop')} desc={t('look.wallpaperHint')}>
			{#snippet control()}
				<span class="wall" style:background={wallpaperPreview} aria-hidden="true"></span>
				<button type="button" class="btn" onclick={chooseWallpaper}>{t('look.wallpaper')}</button>
			{/snippet}
		</SettingRow>
	</SettingGroup>
	{#if canAdminister}
		<AppIconEditor bind:this={iconEditor} bind:dirty={iconDirty} />
	{/if}
</div>

<!-- Dock: the former page /settings/appearance/dock. -->
<div class="tab-panel" hidden={tab !== 'dock'} data-tab-panel="dock">
	<DockSettings />
	<!-- PLAN 79-10: the top bar of the desktop. -->
	{#if barLink.load !== null}
		<SettingGroup title={t('desktop.bar.title')} id="h-bar">
			<SettingRow
				title={t('desktop.bar.load')}
				desc={t('desktop.bar.loadHint')}
				for="bar-load"
				summary={barLink.load ? t('look.on') : t('look.off')}
			>
				{#snippet control()}
					<input
						id="bar-load"
						type="checkbox"
						role="switch"
						class="switch"
						checked={barLink.load}
						onchange={(e) => setBarLoad(e.currentTarget.checked)}
					/>
				{/snippet}
			</SettingRow>
		</SettingGroup>
	{/if}
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
	.liquid-preview {
		position: relative;
		height: 110px;
		margin-bottom: var(--space-1);
		border-radius: 14px;
		overflow: hidden;
		background: url('/backgrounds/nordlicht.webp') center 35% / cover;
		display: grid;
		place-items: center;
	}
	.liquid-preview .pane {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		padding: var(--space-3) 28px;
		border-radius: 18px;
		border: 1px solid color-mix(in srgb, #fff 18%, transparent);
		color: var(--text);
		font-size: var(--text-sm);
	}
	.liquid-preview .pane b {
		font-size: var(--text-xl);
		font-weight: 600;
	}
	.liquid-ends {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.tab-row {
		position: relative;
		display: flex;
		align-items: flex-end;
		gap: var(--space-3);
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
		gap: var(--gap, var(--space-4));
		min-width: 0;
	}
	.tab-panel[hidden] {
		display: none;
	}
	/* Modus: three small previews. */
	.modes {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-3);
	}
	.mode {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0;
		background: none;
		border: 0;
		/* Not muted: on the row's tint the muted colour is too faint in the
		   light theme; the chosen mode is bold and framed. */
		color: var(--text);
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
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.variant {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 32px;
		padding: 0 var(--space-3) 0 var(--space-2);
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
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.color {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2);
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
		gap: var(--space-1-5);
		margin-left: var(--space-1-5);
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
	/* Sliders: the slider with its value at the right of the row. */
	.range {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
		max-width: 100%;
	}
	.range input {
		flex: 1 1 14rem;
		width: 14rem;
		min-width: 0;
		margin: 0;
	}
	.value {
		flex: none;
		min-width: 3.5rem;
		color: var(--muted);
		font-size: var(--text-base);
		font-variant-numeric: tabular-nums;
		text-align: right;
	}
	.group-note {
		margin: var(--space-1-5) 0 0;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.notice-warn {
		margin: 0;
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
		gap: var(--space-2) var(--space-3);
		padding: var(--space-2-5) var(--space-4);
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
		gap: var(--space-2);
	}
	@media (max-width: 639px) {
		.modes,
		.variants,
		.accents,
		.custom {
			justify-content: flex-start;
		}
		.range {
			width: 100%;
		}
		.status {
			position: static;
			max-width: none;
		}
		.tab-row {
			flex-direction: column;
			align-items: stretch;
			gap: var(--space-1);
		}
	}
</style>
