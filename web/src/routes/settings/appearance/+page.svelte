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

	const CUSTOM_FIELDS: (keyof CustomColors)[] = ['bg', 'surface', 'header', 'text', 'border'];
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
	<div class="row">
		<span class="name">
			<label for={id}>{label}</label>
			<span class="muted small">{hint}</span>
		</span>
		<span class="range">
			<input
				{id}
				type="range"
				min={limit.min}
				max={limit.max}
				step={limit.step}
				{value}
				oninput={(e) => set(Number(e.currentTarget.value))}
			/><output for={id} class="mono value">{value} {unit}</output>
		</span>
	</div>
{/snippet}

<PageHead title={t('appearance.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('look.intro')}</p>
	{#snippet actions()}
		<button type="button" class="btn" onclick={reset}>{t('appearance.reset')}</button>
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
</PageHead>

<p class="status muted small" role="status" aria-live="polite" data-state={state_}>
	{#if state_ === 'saving'}{t('ui.saving')}{:else if state_ === 'saved'}{t(
			'appearance.saved',
		)}{:else if state_ === 'error'}<span class="warn">{error}</span>{:else}{t('look.autosave')}{/if}
</p>

<section class="tile panel rows" aria-labelledby="h-colors">
	<h2 class="tile-title" id="h-colors">{t('look.colors')}</h2>
	<div class="row">
		<span class="name">{t('appearance.theme')}</span>
		<Segments label={t('appearance.theme')} options={modes} value={mode} onchange={setMode} />
	</div>
	{#if mode === 'dark'}
		<div class="row">
			<span class="name">{t('look.variant')}</span>
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
							><span style:background={colors.surface}></span></span
						>{t(`theme.${id}`)}
					</button>
				{/each}
			</div>
		</div>
		{#if draft.theme === 'eigen'}
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
							new Intl.NumberFormat(i18n.locale, { maximumFractionDigits: 1 }).format(ratio) + ':1',
					})}
				</p>
			{/if}
		{/if}
	{/if}
	<div class="row">
		<span class="name">{t('appearance.accent')}</span>
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
			<input
				type="color"
				class="pickdot"
				aria-label={t('appearance.customAccent')}
				title={t('appearance.customAccent')}
				class:picked={draft.accent === 'custom'}
				bind:value={draft.customAccent}
				oninput={() => (draft.accent = 'custom')}
			/>
		</div>
	</div>
</section>

<section class="tile panel rows" aria-labelledby="h-sliders">
	<h2 class="tile-title" id="h-sliders">{t('look.sliders')}</h2>
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
</section>

<section class="tile panel rows" aria-labelledby="h-desk">
	<h2 class="tile-title" id="h-desk">{t('look.desktop')}</h2>
	<div class="row">
		<span class="name">{t('desktop.wallpaper.title')}</span>
		<span class="pick">
			<span class="thumb" style:background={wallpaperPreview} aria-hidden="true"></span>
			<button type="button" class="btn" onclick={chooseWallpaper}>{t('look.wallpaper')}</button>
		</span>
	</div>
	<a class="row link" href="/settings/appearance/dock">
		<span class="name"
			><span>{t('look.dock.title')}</span><span class="muted small">{t('look.dock.link')}</span
			></span
		>
		<Icon name="chevronRight" size={18} />
	</a>
</section>

<details class="tile panel more">
	<summary class="tile-title">{t('look.more')}</summary>
	<div class="rows">
		<div class="row">
			<span class="name">{t('appearance.density')}</span>
			<Segments
				label={t('appearance.density')}
				options={[
					{ value: 'normal', label: t('density.normal') },
					{ value: 'kompakt', label: t('density.kompakt') },
				]}
				value={draft.density}
				onchange={(v) => (draft.density = v)}
			/>
		</div>
		<label class="check"
			><input type="checkbox" bind:checked={draft.fx.enabled} />{t('appearance.fx.enabled')}</label
		>
		{#if draft.fx.enabled}
			<div class="fx-list">
				{#each EFFECT_IDS as id (id)}
					<label class="check fx">
						<input type="checkbox" bind:checked={draft.fx[id]} />
						<span
							><span>{t(`appearance.fx.${id}`)}</span><span class="muted small"
								>{t(`appearance.fx.${id}Desc`)}</span
							></span
						>
					</label>
				{/each}
			</div>
		{/if}
		<label class="check"
			><input type="checkbox" bind:checked={draft.motion} />{t('dashboard.visual.smooth')}</label
		>
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
		<p class="muted small">{t('appearance.fx.hint')}</p>
	</div>
</details>

{#if canAdminister}
	<div class="extras">
		<AppIconEditor bind:this={iconEditor} bind:dirty={iconDirty} />
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
	.status {
		margin: 0 0 var(--space-3);
		min-height: 1.4em;
	}
	.warn {
		color: var(--warn, var(--text));
	}
	section + section,
	section + details,
	details + .extras {
		margin-top: var(--space-4);
	}
	.rows {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.row {
		display: grid;
		grid-template-columns: minmax(140px, 240px) 1fr;
		align-items: center;
		gap: var(--space-3);
		min-height: 40px;
	}
	.name {
		display: flex;
		flex-direction: column;
		gap: 2px;
		font-size: var(--text-md);
		min-width: 0;
	}
	.range {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
	}
	.range input {
		flex: 1;
		min-width: 0;
	}
	.value {
		min-width: 56px;
		text-align: right;
		font-size: var(--text-base);
	}
	.small {
		font-size: var(--text-sm);
	}
	.variants {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.variant {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 40px;
		padding: 4px 12px 4px 4px;
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.variant[aria-checked='true'] {
		border-color: var(--accent);
	}
	.swatch {
		width: 30px;
		height: 30px;
		display: flex;
		align-items: flex-end;
		padding: 4px;
		border: 1px solid;
		border-radius: var(--radius-small);
	}
	.swatch span {
		flex: 1;
		height: 10px;
		border-radius: var(--radius-bar);
	}
	.custom {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
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
		width: 32px;
		height: 32px;
		padding: 0;
		border: 0;
		background: transparent;
		cursor: pointer;
	}
	.accents {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.accents .pickdot {
		width: 36px;
		height: 36px;
		padding: 0;
		border: 3px solid transparent;
		border-radius: 50%;
		cursor: pointer;
		box-shadow: 0 0 0 1px var(--border);
	}
	.accents .pickdot[aria-checked='true'],
	.accents .pickdot.picked {
		border-color: var(--text);
	}
	.pick {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-wrap: wrap;
	}
	.thumb {
		width: 96px;
		height: 56px;
		border-radius: var(--radius-ctl);
		border: 1px solid var(--border);
		filter: var(--wallpaper-filter, none);
	}
	.link {
		color: inherit;
		text-decoration: none;
		grid-template-columns: 1fr auto;
		padding: var(--space-2);
		margin: 0 calc(-1 * var(--space-2));
		border-radius: var(--radius-ctl);
	}
	.link:hover,
	.link:focus-visible {
		background: var(--chip);
	}
	.more summary {
		cursor: pointer;
	}
	.more[open] summary {
		margin-bottom: var(--space-3);
	}
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	.fx-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding-left: var(--space-1);
	}
	.check.fx {
		align-items: flex-start;
	}
	.check.fx > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.extras {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	@media (max-width: 600px) {
		.row {
			grid-template-columns: 1fr;
			gap: var(--space-1);
		}
		.link {
			grid-template-columns: 1fr auto;
		}
	}
</style>
