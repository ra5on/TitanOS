<script lang="ts">
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Slider from '#lib/components/Slider.svelte';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { appearance, saveAppearance } from '#lib/theme/appearance.svelte.ts';
	import {
		ACCENT_IDS,
		ACCENTS,
		BACKGROUNDS,
		defaultAppearance,
		DENSITIES,
		EFFECT_IDS,
		LIMITS,
		pickedAccent,
		resolvePalette,
		sanitizeAppearance,
		SURFACE_STYLES,
		textContrast,
		THEME_IDS,
		type Appearance,
		type CustomColors,
		type BarMode,
		type BarSurface,
		type FloatingSurface,
		type ThemeId,
	} from '#lib/theme/themes.ts';
	import Preview from './Preview.svelte';
	import BackgroundEditor from '#lib/dashboard/BackgroundEditor.svelte';
	import { dashboardScene } from '#lib/dashboard/scene.svelte.ts';
	import { dashboardVisual, type DashboardVisual } from '#lib/dashboard/visual.ts';
	import { session } from '#lib/session.svelte.ts';
	import LoginAppearanceEditor from '#lib/login/LoginAppearanceEditor.svelte';
	import AppIconEditor from '#lib/components/AppIconEditor.svelte';
	import { onDestroy } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { arrangeNav, moveNav, navForRole, NAV_BUTTONS, NAV_FIXED } from '#lib/navigation.ts';

	const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
	const CUSTOM_FIELDS: (keyof CustomColors)[] = ['bg', 'surface', 'header', 'text', 'border'];

	// What is being edited. The rest of the UI keeps the saved appearance
	// until "Apply".
	let draft = $state<Appearance>($state.snapshot(appearance.current));
	let saving = $state(false);
	let message = $state<{ text: string; warn: boolean } | null>(null);
	// Errors of the last "Apply", one line per area that could not be saved.
	let failures = $state<string[]>([]);
	// The app symbol and the login page (owners/admins) are saved by the same bar.
	let iconEditor = $state<AppIconEditor>();
	let loginEditor = $state<LoginAppearanceEditor>();
	let iconDirty = $state(false);
	let loginDirty = $state(false);
	let themeFile: HTMLInputElement;
	let imageFile = $state<HTMLInputElement>();

	// The dashboard background lives in the dashboard preferences (with the
	// account's images); it is saved with "Apply" like the rest.
	let visual = $state<DashboardVisual>(dashboardVisual(session.dashboard?.visual));
	const pendingImages = new Map<string, File>();
	const visualDirty = $derived(
		JSON.stringify(visual) !== JSON.stringify(dashboardVisual(session.dashboard?.visual)),
	);
	onDestroy(() => {
		for (const url of Object.values(dashboardScene.urls)) URL.revokeObjectURL(url);
		dashboardScene.urls = {};
	});

	// The login page is one setting for the whole NAS.
	const navItems = $derived(navForRole(session.data?.user?.role ?? 'user'));
	const navArranged = $derived(arrangeNav(navItems, draft.nav));
	function toggleNav(id: string, shown: boolean) {
		draft.nav.hidden = shown
			? draft.nav.hidden.filter((other) => other !== id)
			: [...draft.nav.hidden, id];
	}

	const canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	const appearanceDirty = $derived(JSON.stringify(draft) !== JSON.stringify(appearance.current));
	const dirty = $derived(appearanceDirty || visualDirty || iconDirty || loginDirty);
	const palette = $derived(resolvePalette(draft, appearance.systemLight));
	const ratio = $derived(textContrast(palette));

	const previewLabel = $derived.by(() => {
		if (draft.theme === 'auto')
			return t(
				appearance.systemLight ? 'appearance.previewAutoLight' : 'appearance.previewAutoDark',
			);
		if (draft.style === 'glass' && draft.bg === 'solid') return t('appearance.previewGlassHint');
		return t('appearance.previewDefault');
	});

	/** The colors a theme card shows. */
	function look(id: ThemeId) {
		return resolvePalette({ ...draft, theme: id }, appearance.systemLight);
	}

	/** "Like the tiles" or own values; own values start from the tiles' ones. */
	function setOwn(surface: FloatingSurface, own: boolean) {
		if (own && !surface.own) {
			surface.glass = draft.glass;
			surface.blur = draft.blur;
			surface.shine = draft.shine;
		}
		surface.own = own;
	}
	const FLOATING_GROUPS = ['menus', 'dialogs'] as const;
	const ownOptions = $derived([
		{ value: 'inherit', label: t('appearance.floating.inherit') },
		{ value: 'own', label: t('appearance.floating.own') },
	]);
	// Header/status bar and side navigation: like the tiles, opaque or own values.
	const barOptions = $derived([
		{ value: 'tiles', label: t('appearance.floating.inherit') },
		{ value: 'opaque', label: t('appearance.floating.opaqueBars') },
		{ value: 'own', label: t('appearance.floating.own') },
	]);
	/** Own values start from what the bar showed before (tiles' values). */
	function setBarMode(bar: BarSurface, mode: BarMode) {
		if (mode === 'own' && bar.mode !== 'own') {
			bar.glass = draft.glass;
			bar.blur = draft.blur;
			bar.shine = draft.shine;
		}
		bar.mode = mode;
		bar.own = mode === 'own';
	}

	function fail(err: unknown) {
		message = { text: apiErrorText(err), warn: true };
	}

	/**
	 * The page's one "Apply": saves every area with changes (appearance,
	 * dashboard background, app symbol, login page). One failing area does not
	 * stop the others; each failure is named with its area.
	 */
	async function apply() {
		saving = true;
		message = null;
		failures = [];
		const steps: [string, boolean, () => Promise<unknown>][] = [
			[t('appearance.title'), appearanceDirty, () => saveAppearance($state.snapshot(draft))],
			[t('dashboard.visual.background'), visualDirty, saveVisual],
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
		if (!failures.length) message = { text: t('appearance.saved'), warn: false };
		saving = false;
	}

	/** Drops every unsaved change on the page. */
	function discard() {
		draft = $state.snapshot(appearance.current);
		visual = dashboardVisual(session.dashboard?.visual);
		pendingImages.clear();
		iconEditor?.discard();
		loginEditor?.discard();
		message = null;
		failures = [];
	}

	async function saveVisual() {
		const preferences = { ...session.dashboard, visual: $state.snapshot(visual) };
		const uploads = visual.images.filter((image) => pendingImages.has(image.id));
		if (uploads.length) {
			const form = new FormData();
			form.append('preferences', JSON.stringify(preferences));
			for (const image of uploads) form.append('image', pendingImages.get(image.id)!, image.id);
			await api.put('/preferences/dashboard', form);
		} else await api.put('/preferences/dashboard', preferences);
		pendingImages.clear();
		session.dashboard = preferences;
	}

	function reset() {
		// The uploaded image stays on the NAS; only the settings go back.
		draft = { ...defaultAppearance(), bgVersion: draft.bgVersion };
		message = null;
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
			draft = { ...sanitizeAppearance(data), bgVersion: draft.bgVersion };
			message = null;
		} catch {
			message = { text: t('appearance.importFailed'), warn: true };
		}
	}

	async function uploadImage() {
		const file = imageFile?.files?.[0];
		if (imageFile) imageFile.value = '';
		if (!file) return;
		if (file.size > MAX_IMAGE_BYTES) {
			message = { text: errorText('too_large'), warn: true };
			return;
		}
		message = null;
		try {
			await api.put('/background', file);
			draft.bgVersion = Date.now();
		} catch (err) {
			fail(err);
		}
	}

	async function removeImage() {
		message = null;
		try {
			await api.delete('/background');
			draft.bgVersion = 0;
		} catch (err) {
			fail(err);
		}
	}
</script>

{#snippet barFields(key: 'bars' | 'sidebar', label: string, shine: boolean)}
	<Segments
		{label}
		options={barOptions}
		value={draft[key].mode}
		onchange={(value) => setBarMode(draft[key], value as BarMode)}
	/>
	{#if draft[key].mode === 'own'}
		<Slider
			id="{key}-glass"
			label={t('appearance.transparency')}
			bind:value={draft[key].glass}
			limit={LIMITS.glass}
			unit="%"
		/>
		<Slider
			id="{key}-blur"
			label={t('appearance.blur')}
			bind:value={draft[key].blur}
			limit={LIMITS.blur}
			unit="px"
		/>
		{#if shine}<Slider
				id="{key}-shine"
				label={t('appearance.shine')}
				bind:value={draft[key].shine}
				limit={LIMITS.shine}
				unit="%"
			/>{/if}
	{/if}
{/snippet}

<PageHead title={t('appearance.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('appearance.intro')}</p>
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

<div class="columns">
	<div class="editor">
		<section class="tile panel" aria-labelledby="h-theme">
			<h2 class="tile-title" id="h-theme">{t('appearance.theme')}</h2>
			<div class="themes" role="radiogroup" aria-labelledby="h-theme">
				{#each THEME_IDS as id (id)}
					{@const colors = look(id)}
					<button
						type="button"
						class="option theme"
						role="radio"
						aria-checked={draft.theme === id}
						onclick={() => (draft.theme = id)}
					>
						<span
							class="swatch"
							aria-hidden="true"
							style:background={colors.bg}
							style:border-color={colors.border}
						>
							<span style:background={colors.surface}><i></i></span>
							<span style:background={colors.surface}></span>
						</span>
						<span class="option-name">{t(`theme.${id}`)}</span>
					</button>
				{/each}
			</div>
			{#if draft.theme === 'eigen'}
				<div class="custom">
					{#each CUSTOM_FIELDS as field (field)}
						<label class="color">
							<input type="color" bind:value={draft.custom[field]} />
							<span
								><span>{t(`custom.${field}`)}</span><span class="mono muted"
									>{draft.custom[field].toUpperCase()}</span
								></span
							>
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
			{/if}
		</section>

		<section class="tile panel" aria-labelledby="h-accent">
			<h2 class="tile-title" id="h-accent">{t('appearance.accent')}</h2>
			<div class="accents" role="radiogroup" aria-labelledby="h-accent">
				{#each ACCENT_IDS as id (id)}
					<div class="accent">
						<button
							type="button"
							role="radio"
							aria-label={t(`accent.${id}`)}
							aria-checked={draft.accent === id}
							style:background={ACCENTS[id].hex}
							onclick={() => (draft.accent = id)}
						></button>
						<span class:muted={draft.accent !== id}>{t(`accent.${id}`)}</span>
					</div>
				{/each}
				<label class="accent">
					<input
						type="color"
						aria-label={t('appearance.customAccent')}
						class:picked={draft.accent === 'custom'}
						bind:value={draft.customAccent}
						oninput={() => (draft.accent = 'custom')}
					/>
					<span class:muted={draft.accent !== 'custom'}>{t('accent.custom')}</span>
				</label>
			</div>
			<p class="hint muted">{t('appearance.accentHint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="h-style">
			<h2 class="tile-title" id="h-style">{t('appearance.style')}</h2>
			<div class="three" role="radiogroup" aria-labelledby="h-style">
				{#each SURFACE_STYLES as id (id)}
					<button
						type="button"
						class="option"
						role="radio"
						aria-checked={draft.style === id}
						onclick={() => (draft.style = id)}
					>
						<span class="option-name">{t(`style.${id}`)}</span>
						<span class="muted small">{t(`style.${id}Desc`)}</span>
					</button>
				{/each}
			</div>
			{#if draft.style === 'glass'}
				<Slider
					id="glass"
					label={t('appearance.transparency')}
					bind:value={draft.glass}
					limit={LIMITS.glass}
					unit="%"
				/>
				<Slider
					id="blur"
					label={t('appearance.blur')}
					bind:value={draft.blur}
					limit={LIMITS.blur}
					unit="px"
				/>
				<Slider
					id="shine"
					label={t('appearance.shine')}
					bind:value={draft.shine}
					limit={LIMITS.shine}
					unit="%"
				/>
				<p class="hint muted">{t('appearance.glassHint')}</p>
			{/if}
		</section>

		<section class="tile panel" aria-labelledby="h-floating" id="menus">
			<h2 class="tile-title" id="h-floating">{t('appearance.floating.title')}</h2>
			{#if draft.style === 'glass'}
				{#each FLOATING_GROUPS as key (key)}
					<fieldset class:divided={key !== 'menus'}>
						<legend>{t(`appearance.floating.${key}`)}</legend>
						<p class="hint muted">{t(`appearance.floating.${key}Desc`)}</p>
						<Segments
							label={t(`appearance.floating.${key}`)}
							options={ownOptions}
							value={draft[key].own ? 'own' : 'inherit'}
							onchange={(value) => setOwn(draft[key], value === 'own')}
						/>
						{#if draft[key].own}
							<Slider
								id="{key}-glass"
								label={t('appearance.transparency')}
								bind:value={draft[key].glass}
								limit={LIMITS.glass}
								unit="%"
							/>
							<Slider
								id="{key}-blur"
								label={t('appearance.blur')}
								bind:value={draft[key].blur}
								limit={LIMITS.blur}
								unit="px"
							/>
							<Slider
								id="{key}-shine"
								label={t('appearance.shine')}
								bind:value={draft[key].shine}
								limit={LIMITS.shine}
								unit="%"
							/>
						{/if}
					</fieldset>
				{/each}
				<fieldset class="divided">
					<legend>{t('appearance.floating.bars')}</legend>
					<p class="hint muted">{t('appearance.floating.barsDesc')}</p>
					{@render barFields('bars', t('appearance.floating.bars'), false)}
				</fieldset>
				<p class="hint muted">{t('appearance.floating.hint')}</p>
			{:else}
				<p class="hint muted">{t('appearance.floating.opaque')}</p>
			{/if}
			<div class:divided={draft.style === 'glass'}>
				<Slider
					id="dialog-dim"
					label={t('appearance.floating.dim')}
					bind:value={draft.dialogs.dim}
					limit={LIMITS.dialogDim}
					unit="%"
				/>
			</div>
		</section>

		<section class="tile panel" aria-labelledby="h-shape">
			<h2 class="tile-title" id="h-shape">{t('appearance.shape')}</h2>
			<Slider
				id="r-tile"
				label={t('appearance.radiusTile')}
				bind:value={draft.rTile}
				limit={LIMITS.rTile}
				unit="px"
			/>
			<Slider
				id="r-ctl"
				label={t('appearance.radiusCtl')}
				bind:value={draft.rCtl}
				limit={LIMITS.rCtl}
				unit="px"
			/>
			<Slider
				id="font"
				label={t('appearance.fontSize')}
				bind:value={draft.font}
				limit={LIMITS.font}
				unit="%"
			/>
			<div class="density">
				<span id="h-density">{t('appearance.density')}</span>
				<div class="segments" role="radiogroup" aria-labelledby="h-density">
					{#each DENSITIES as id (id)}
						<button
							type="button"
							role="radio"
							aria-checked={draft.density === id}
							onclick={() => (draft.density = id)}>{t(`density.${id}`)}</button
						>
					{/each}
				</div>
			</div>
		</section>

		<section class="tile panel" aria-labelledby="h-effects">
			<h2 class="tile-title" id="h-effects">{t('appearance.effects')}</h2>
			<Slider
				id="glow"
				label={t('dashboard.visual.glow')}
				bind:value={draft.glow}
				limit={LIMITS.glow}
				unit="%"
			/>
			<p class="hint muted">{t('dashboard.visual.glowHint')}</p>
			{#if draft.glow > 0}
				<label class="check"
					><input
						type="checkbox"
						checked={!draft.glowColor}
						onchange={(e) => (draft.glowColor = e.currentTarget.checked ? '' : pickedAccent(draft))}
					/>{t('appearance.accent')}</label
				>
				{#if draft.glowColor}
					<label class="color-field"
						><input
							type="color"
							value={draft.glowColor}
							oninput={(e) => (draft.glowColor = e.currentTarget.value.toUpperCase())}
						/><span>{t('dashboard.visual.glowColor')}</span></label
					>
				{/if}
			{/if}
			<label class="check divided"
				><input type="checkbox" bind:checked={draft.motion} />{t('dashboard.visual.smooth')}</label
			>
			{#if draft.motion}
				<Slider
					id="duration"
					label={t('dashboard.visual.duration')}
					bind:value={draft.duration}
					limit={LIMITS.duration}
					unit="ms"
				/>
			{/if}
			<p class="hint muted">{t('dashboard.visual.motionHint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="h-fx" id="effects">
			<h2 class="tile-title" id="h-fx">{t('appearance.fx.title')}</h2>
			<label class="check"
				><input type="checkbox" bind:checked={draft.fx.enabled} />{t(
					'appearance.fx.enabled',
				)}</label
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
			<p class="hint muted">{t('appearance.fx.hint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="h-nav" id="navigation">
			<h2 class="tile-title" id="h-nav">{t('appearance.nav.title')}</h2>
			<fieldset class="layouts">
				<legend>{t('appearance.nav.layout')}</legend>
				{#each ['top', 'side'] as const as layout (layout)}
					<label class="layout" class:selected={draft.nav.layout === layout}
						><input
							type="radio"
							name="nav-layout"
							value={layout}
							bind:group={draft.nav.layout}
						/><span class="sketch {layout}" aria-hidden="true"><i></i><i></i></span><strong
							>{t(`appearance.nav.${layout}`)}</strong
						><span class="muted small">{t(`appearance.nav.${layout}Hint`)}</span></label
					>
				{/each}
			</fieldset>
			{#if draft.nav.layout === 'side'}
				<fieldset class="sidebar-look" id="sidebar">
					<legend>{t('appearance.sidebar.title')}</legend>
					{#if draft.style === 'glass'}
						<p class="hint muted">{t('appearance.sidebar.desc')}</p>
						{@render barFields('sidebar', t('appearance.sidebar.title'), true)}
					{:else}
						<p class="hint muted">{t('appearance.sidebar.opaque')}</p>
					{/if}
				</fieldset>
			{/if}
			{#if draft.nav.layout === 'side'}<label class="check magnify"
					><input type="checkbox" bind:checked={draft.nav.magnify} />{t(
						'appearance.nav.magnify',
					)}</label
				>{/if}
			{#if draft.nav.layout === 'side'}
				<fieldset class="corners">
					<legend>{t('appearance.nav.button')}</legend>
					<p class="hint muted">{t('appearance.nav.buttonHint')}</p>
					<div class="corner-list">
						{#each NAV_BUTTONS as corner (corner)}
							<label class="corner" class:selected={draft.nav.button === corner}
								><input
									type="radio"
									name="nav-button"
									value={corner}
									bind:group={draft.nav.button}
								/><span class="phone {corner}" aria-hidden="true"><i></i></span><span
									>{t(`appearance.nav.button.${corner}`)}</span
								></label
							>
						{/each}
					</div>
				</fieldset>
			{/if}
			<ol class="nav-list">
				{#each navArranged as item, index (item.id)}
					<li>
						<label class="check">
							<input
								type="checkbox"
								checked={item.id === NAV_FIXED || !draft.nav.hidden.includes(item.id)}
								disabled={item.id === NAV_FIXED}
								onchange={(event) => toggleNav(item.id, event.currentTarget.checked)}
							/>{t(item.label)}{#if item.id === NAV_FIXED}<span class="muted small"
									>{t('appearance.nav.fixed')}</span
								>{/if}
						</label>
						<button
							type="button"
							class="btn small"
							disabled={index === 0}
							aria-label={t('appearance.nav.up', { tab: t(item.label) })}
							title={t('appearance.nav.up', { tab: t(item.label) })}
							onclick={() => (draft.nav.order = moveNav(navItems, draft.nav, item.id, -1))}
							><Icon name="arrowUp" size={14} /></button
						>
						<button
							type="button"
							class="btn small"
							disabled={index === navArranged.length - 1}
							aria-label={t('appearance.nav.down', { tab: t(item.label) })}
							title={t('appearance.nav.down', { tab: t(item.label) })}
							onclick={() => (draft.nav.order = moveNav(navItems, draft.nav, item.id, 1))}
							><Icon name="arrowDown" size={14} /></button
						>
					</li>
				{/each}
			</ol>
			<div class="row-actions">
				<button
					type="button"
					class="btn small"
					onclick={() => (draft.nav = { ...draft.nav, order: [], hidden: [] })}
					>{t('appearance.nav.reset')}</button
				>
			</div>
			<p class="hint muted">{t('appearance.nav.hint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="h-bg">
			<h2 class="tile-title" id="h-bg">{t('appearance.background')}</h2>
			<div class="three" role="radiogroup" aria-labelledby="h-bg">
				{#each BACKGROUNDS as id (id)}
					<button
						type="button"
						class="option center"
						role="radio"
						aria-checked={draft.bg === id}
						onclick={() => (draft.bg = id)}
					>
						<span class="option-name">{t(`bg.${id}`)}</span>
					</button>
				{/each}
			</div>
			{#if draft.bg === 'image'}
				<div class="upload">
					<span class="muted"
						>{draft.bgVersion > 0
							? t('appearance.imageHint')
							: `${t('appearance.imageNone')} ${t('appearance.imageHint')}`}</span
					>
					<button type="button" class="btn" onclick={() => imageFile?.click()}
						>{t('appearance.chooseImage')}</button
					>
					{#if draft.bgVersion > 0}
						<button type="button" class="btn" onclick={removeImage}
							>{t('appearance.removeImage')}</button
						>
					{/if}
					<input
						bind:this={imageFile}
						type="file"
						accept="image/jpeg,image/png,image/webp"
						hidden
						onchange={uploadImage}
					/>
				</div>
				<Slider
					id="dim"
					label={t('appearance.dim')}
					bind:value={draft.dim}
					limit={LIMITS.dim}
					unit="%"
				/>
			{/if}
		</section>

		<BackgroundEditor
			value={visual}
			urls={dashboardScene.urls}
			onbegin={() => {}}
			onchange={(v) => (visual = v)}
			onpreview={() => {}}
			onupload={(id, file, url) => {
				const previous = dashboardScene.urls[id];
				if (previous) URL.revokeObjectURL(previous);
				pendingImages.set(id, file);
				dashboardScene.urls = { ...dashboardScene.urls, [id]: url };
			}}
		/>

		{#if canAdminister}<AppIconEditor
				bind:this={iconEditor}
				bind:dirty={iconDirty}
			/><LoginAppearanceEditor bind:this={loginEditor} bind:dirty={loginDirty} />{/if}
	</div>

	<section class="preview" aria-labelledby="h-preview">
		<div class="preview-head">
			<h2 class="tile-title" id="h-preview">{t('appearance.preview')}</h2>
			<span class="muted small">{previewLabel}</span>
		</div>
		<Preview {draft} />
	</section>
</div>

<!-- One bar for the whole page; it sticks to the bottom while something is unsaved. -->
{#if dirty || message || failures.length}<div
		class="save-bar"
		class:pinned={dirty}
		class:notice-warn={dirty}
	>
		<span class="state" class:warn={message?.warn} role="status"
			>{#if saving}{t('ui.saving')}{:else if dirty}{t(
					'appearance.unsaved',
				)}{:else if message}{message.text}{/if}</span
		>
		{#each failures as failure (failure)}<p class="notice-error" role="alert">{failure}</p>{/each}
		{#if message?.warn && dirty}<p class="notice-error" role="alert">{message.text}</p>{/if}
		{#if dirty}<div class="row-actions">
				<button type="button" class="btn" disabled={saving} onclick={discard}
					>{t('appearance.discard')}</button
				><button type="button" class="btn btn-primary" disabled={saving} onclick={apply}
					>{t('appearance.apply')}</button
				>
			</div>{/if}
	</div>{/if}

<style>
	.fx-list {
		display: flex;
		flex-direction: column;
		gap: 10px;
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
	.color-field {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
	}
	.color-field input {
		width: 42px;
		height: 28px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: none;
	}
	.columns {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-5);
	}
	.editor {
		flex: 1 1 460px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.preview {
		flex: 1 1 520px;
		min-width: 0;
		position: sticky;
		top: 84px;
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.preview-head {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		gap: var(--space-3);
	}
	.small {
		font-size: var(--text-sm);
	}
	.hint {
		font-size: var(--text-sm);
	}

	.themes {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
		gap: 10px;
	}
	.three {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: var(--space-2);
	}
	/* On phones each choice gets its own row, so no description is cut. */
	@media (max-width: 520px) {
		.three {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-3);
		text-align: left;
		background: var(--bg);
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.option[aria-checked='true'] {
		border-color: var(--accent);
	}
	.option.theme {
		gap: var(--space-2);
		padding: var(--space-2);
	}
	.option.center {
		align-items: center;
	}
	.option-name {
		font-size: var(--text-base);
		font-weight: 600;
	}
	.swatch {
		height: 44px;
		display: flex;
		gap: var(--space-1);
		padding: 6px;
		border: 1px solid;
		border-radius: var(--radius-small);
	}
	.swatch span {
		flex: 1 1 0;
		display: flex;
		align-items: flex-end;
		padding: 3px;
		border-radius: var(--radius-bar);
	}
	.swatch i {
		display: block;
		width: 70%;
		height: 3px;
		border-radius: var(--radius-bar);
		background: var(--accent);
	}

	.custom {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 10px;
		padding-top: 6px;
	}
	.color {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: var(--space-2) 10px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
		cursor: pointer;
	}
	.color > span {
		display: flex;
		flex-direction: column;
	}
	.color .mono {
		font-size: var(--text-xs);
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
		align-items: flex-start;
		gap: var(--space-3);
	}
	.accent {
		width: 58px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.accent button,
	.accent input {
		width: 44px;
		height: 44px;
		border: 3px solid transparent;
		border-radius: 50%;
		cursor: pointer;
	}
	.accent button[aria-checked='true'],
	.accent input.picked {
		border-color: var(--text);
	}

	.density {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		font-size: var(--text-md);
	}
	.segments {
		display: flex;
		gap: var(--space-1);
		padding: 3px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 34px;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}

	.upload {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		padding: 10px var(--space-3);
		background: var(--bg);
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
	}
	.upload span {
		flex: 1 1 200px;
	}
	.upload .btn {
		height: 2.25rem;
	}

	.save-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		margin-top: var(--space-4);
	}
	.save-bar .row-actions {
		margin-left: auto;
	}
	/* Only unsaved changes stick to the bottom (like the firewall's bar). */
	.save-bar.pinned {
		position: sticky;
		bottom: 12px;
		z-index: 5;
		/* Beside the round menu button of "Side" navigation on a phone. */
		margin-left: var(--fab-clear, 0px);
	}
	.state {
		font-size: var(--text-base);
		color: var(--muted);
	}
	.state.warn {
		color: var(--warn-text);
	}

	@media (max-width: 1100px) {
		.preview {
			position: static;
		}
	}
	.corners {
		border: 0;
		margin: 0;
		padding: 0;
		min-width: 0;
	}
	.corners legend {
		font-weight: 600;
		padding: 0;
	}
	.corner-list {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 7rem));
		gap: var(--space-2);
		margin-top: var(--space-2);
	}
	.corner {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		padding: 8px 4px;
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 10px);
		cursor: pointer;
		font-size: var(--text-sm);
		text-align: center;
	}
	.corner.selected {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}
	.corner input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.corner:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.phone {
		position: relative;
		width: 34px;
		height: 58px;
		border: 2px solid var(--muted);
		border-radius: 8px;
	}
	.phone i {
		position: absolute;
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: var(--accent);
	}
	.phone.tl i,
	.phone.tr i {
		top: 4px;
	}
	.phone.bl i,
	.phone.br i {
		bottom: 4px;
	}
	.phone.tl i,
	.phone.bl i {
		left: 4px;
	}
	.phone.tr i,
	.phone.br i {
		right: 4px;
	}
	.layouts {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--space-3);
		margin: 0 0 var(--space-3);
		padding: 0;
		border: 0;
	}
	/* The legend takes a grid cell of its own here; give it the whole row,
	   so that the two sketches stand side by side below it. */
	.layouts legend {
		grid-column: 1 / -1;
		margin-bottom: var(--space-2);
		padding: 0;
		font-weight: 500;
	}
	.layout {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.layout.selected {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.layout input {
		position: absolute;
		top: 12px;
		right: 12px;
		accent-color: var(--accent);
	}
	.layout:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.sketch {
		position: relative;
		height: 64px;
		margin-bottom: 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--bg);
	}
	.sketch i {
		position: absolute;
		border-radius: 3px;
		background: color-mix(in srgb, var(--text) 10%, transparent);
	}
	.sketch i:first-child {
		background: color-mix(in srgb, var(--accent) 45%, transparent);
	}
	.sketch.top i:first-child {
		inset: 5px 5px auto;
		height: 9px;
	}
	.sketch.top i:last-child {
		inset: 19px 5px 5px;
	}
	.sketch.side i:first-child {
		inset: 5px auto 5px 5px;
		width: 26px;
	}
	.sketch.side i:last-child {
		inset: 5px 5px 5px 36px;
	}
	.nav-list {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.nav-list li {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 3px 0;
	}
	.nav-list label {
		flex: 1;
		gap: var(--space-2);
	}
</style>
