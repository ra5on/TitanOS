<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Slider from '#lib/components/Slider.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { appearance, saveAppearance } from '#lib/theme/appearance.svelte.ts';
	import {
		ACCENT_IDS,
		ACCENTS,
		BACKGROUNDS,
		defaultAppearance,
		DENSITIES,
		LIMITS,
		resolvePalette,
		sanitizeAppearance,
		SURFACE_STYLES,
		textContrast,
		THEME_IDS,
		type Appearance,
		type CustomColors,
		type ThemeId
	} from '#lib/theme/themes.ts';
	import Preview from './Preview.svelte';

	const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
	const CUSTOM_FIELDS: (keyof CustomColors)[] = ['bg', 'surface', 'header', 'text', 'border'];

	// What is being edited. The rest of the UI keeps the saved appearance
	// until "Apply".
	let draft = $state<Appearance>($state.snapshot(appearance.current));
	let saving = $state(false);
	let message = $state<{ text: string; warn: boolean } | null>(null);
	let themeFile: HTMLInputElement;
	let imageFile = $state<HTMLInputElement>();

	const dirty = $derived(JSON.stringify(draft) !== JSON.stringify(appearance.current));
	const palette = $derived(resolvePalette(draft, appearance.systemLight));
	const ratio = $derived(textContrast(palette));

	const previewLabel = $derived.by(() => {
		if (draft.theme === 'auto') return t(appearance.systemLight ? 'appearance.previewAutoLight' : 'appearance.previewAutoDark');
		if (draft.style === 'glass' && draft.bg === 'solid') return t('appearance.previewGlassHint');
		return t('appearance.previewDefault');
	});

	/** The colors a theme card shows. */
	function look(id: ThemeId) {
		return resolvePalette({ ...draft, theme: id }, appearance.systemLight);
	}

	function fail(err: unknown) {
		message = { text: errorText(err instanceof ApiError ? err.code : 'internal'), warn: true };
	}

	async function apply() {
		saving = true;
		message = null;
		try {
			await saveAppearance($state.snapshot(draft));
			message = { text: t('appearance.saved'), warn: false };
		} catch (err) {
			fail(err);
		} finally {
			saving = false;
		}
	}

	function reset() {
		// The uploaded image stays on the NAS; only the settings go back.
		draft = { ...defaultAppearance(), bgVersion: draft.bgVersion };
		message = null;
	}

	function exportTheme() {
		const { bgVersion: _image, ...theme } = $state.snapshot(draft);
		const blob = new Blob([JSON.stringify({ titanosTheme: 1, ...theme }, null, '\t') + '\n'], { type: 'application/json' });
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
			if (typeof data !== 'object' || data === null || !('titanosTheme' in data)) throw new Error('not a theme');
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

<PageHead title={t('appearance.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('appearance.intro')}</p>
	{#snippet actions()}
		<button type="button" class="btn" onclick={exportTheme}>{t('appearance.export')}</button>
		<button type="button" class="btn" onclick={() => themeFile.click()}>{t('appearance.import')}</button>
		<input bind:this={themeFile} type="file" accept="application/json,.json" hidden onchange={importTheme} />
	{/snippet}
</PageHead>

<div class="columns">
	<div class="editor">
		<section class="tile panel" aria-labelledby="h-theme">
			<h2 class="tile-title" id="h-theme">{t('appearance.theme')}</h2>
			<div class="themes" role="radiogroup" aria-labelledby="h-theme">
				{#each THEME_IDS as id (id)}
					{@const colors = look(id)}
					<button type="button" class="option theme" role="radio" aria-checked={draft.theme === id} onclick={() => (draft.theme = id)}>
						<span class="swatch" aria-hidden="true" style:background={colors.bg} style:border-color={colors.border}>
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
							<span><span>{t(`custom.${field}`)}</span><span class="mono muted">{draft.custom[field].toUpperCase()}</span></span>
						</label>
					{/each}
				</div>
				{#if ratio < 4.5}
					<p class="notice-warn">
						{t('appearance.contrastWarning', { ratio: new Intl.NumberFormat(i18n.locale, { maximumFractionDigits: 1 }).format(ratio) + ':1' })}
					</p>
				{/if}
			{/if}
		</section>

		<section class="tile panel" aria-labelledby="h-accent">
			<h2 class="tile-title" id="h-accent">{t('appearance.accent')}</h2>
			<div class="accents" role="radiogroup" aria-labelledby="h-accent">
				{#each ACCENT_IDS as id (id)}
					<div class="accent">
						<button type="button" role="radio" aria-label={t(`accent.${id}`)} aria-checked={draft.accent === id} style:background={ACCENTS[id].hex} onclick={() => (draft.accent = id)}></button>
						<span class:muted={draft.accent !== id}>{t(`accent.${id}`)}</span>
					</div>
				{/each}
				<label class="accent">
					<input type="color" aria-label={t('appearance.customAccent')} class:picked={draft.accent === 'custom'} bind:value={draft.customAccent} oninput={() => (draft.accent = 'custom')} />
					<span class:muted={draft.accent !== 'custom'}>{t('accent.custom')}</span>
				</label>
			</div>
			<p class="hint muted">{t('appearance.accentHint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="h-style">
			<h2 class="tile-title" id="h-style">{t('appearance.style')}</h2>
			<div class="three" role="radiogroup" aria-labelledby="h-style">
				{#each SURFACE_STYLES as id (id)}
					<button type="button" class="option" role="radio" aria-checked={draft.style === id} onclick={() => (draft.style = id)}>
						<span class="option-name">{t(`style.${id}`)}</span>
						<span class="muted small">{t(`style.${id}Desc`)}</span>
					</button>
				{/each}
			</div>
			{#if draft.style === 'glass'}
				<Slider id="glass" label={t('appearance.transparency')} bind:value={draft.glass} limit={LIMITS.glass} unit="%" />
				<Slider id="blur" label={t('appearance.blur')} bind:value={draft.blur} limit={LIMITS.blur} unit="px" />
				<Slider id="shine" label={t('appearance.shine')} bind:value={draft.shine} limit={LIMITS.shine} unit="%" />
				<p class="hint muted">{t('appearance.glassHint')}</p>
			{/if}
		</section>

		<section class="tile panel" aria-labelledby="h-shape">
			<h2 class="tile-title" id="h-shape">{t('appearance.shape')}</h2>
			<Slider id="r-tile" label={t('appearance.radiusTile')} bind:value={draft.rTile} limit={LIMITS.rTile} unit="px" />
			<Slider id="r-ctl" label={t('appearance.radiusCtl')} bind:value={draft.rCtl} limit={LIMITS.rCtl} unit="px" />
			<Slider id="font" label={t('appearance.fontSize')} bind:value={draft.font} limit={LIMITS.font} unit="%" />
			<div class="density">
				<span id="h-density">{t('appearance.density')}</span>
				<div class="segments" role="radiogroup" aria-labelledby="h-density">
					{#each DENSITIES as id (id)}
						<button type="button" role="radio" aria-checked={draft.density === id} onclick={() => (draft.density = id)}>{t(`density.${id}`)}</button>
					{/each}
				</div>
			</div>
		</section>

		<section class="tile panel" aria-labelledby="h-bg">
			<h2 class="tile-title" id="h-bg">{t('appearance.background')}</h2>
			<div class="three" role="radiogroup" aria-labelledby="h-bg">
				{#each BACKGROUNDS as id (id)}
					<button type="button" class="option center" role="radio" aria-checked={draft.bg === id} onclick={() => (draft.bg = id)}>
						<span class="option-name">{t(`bg.${id}`)}</span>
					</button>
				{/each}
			</div>
			{#if draft.bg === 'image'}
				<div class="upload">
					<span class="muted">{draft.bgVersion > 0 ? t('appearance.imageHint') : `${t('appearance.imageNone')} ${t('appearance.imageHint')}`}</span>
					<button type="button" class="btn" onclick={() => imageFile?.click()}>{t('appearance.chooseImage')}</button>
					{#if draft.bgVersion > 0}
						<button type="button" class="btn" onclick={removeImage}>{t('appearance.removeImage')}</button>
					{/if}
					<input bind:this={imageFile} type="file" accept="image/jpeg,image/png,image/webp" hidden onchange={uploadImage} />
				</div>
				<Slider id="dim" label={t('appearance.dim')} bind:value={draft.dim} limit={LIMITS.dim} unit="%" />
			{/if}
		</section>

		<div class="apply">
			<button type="button" class="btn btn-primary" onclick={apply} disabled={saving || !dirty}>{t('appearance.apply')}</button>
			<button type="button" class="btn" onclick={reset}>{t('appearance.reset')}</button>
			<span class="state" class:warn={message?.warn} role="status">
				{#if message}{message.text}{:else if dirty}{t('appearance.unsaved')}{/if}
			</span>
		</div>
	</div>

	<section class="preview" aria-labelledby="h-preview">
		<div class="preview-head">
			<h2 class="tile-title" id="h-preview">{t('appearance.preview')}</h2>
			<span class="muted small">{previewLabel}</span>
		</div>
		<Preview {draft} />
	</section>
</div>

<style>
	.columns {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 24px;
	}
	.editor {
		flex: 1 1 460px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--gap);
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
		gap: 12px;
	}
	.small {
		font-size: 0.75rem;
	}
	.hint {
		font-size: 0.75rem;
	}

	.themes {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
		gap: 10px;
	}
	.three {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 8px;
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px;
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
		gap: 8px;
		padding: 8px;
	}
	.option.center {
		align-items: center;
	}
	.option-name {
		font-size: 0.8125rem;
		font-weight: 600;
	}
	.swatch {
		height: 44px;
		display: flex;
		gap: 4px;
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
	}
	.color {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 10px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.color > span {
		display: flex;
		flex-direction: column;
	}
	.color .mono {
		font-size: 0.6875rem;
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
		gap: 12px;
	}
	.accent {
		width: 58px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		font-size: 0.75rem;
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
		font-size: 0.875rem;
	}
	.segments {
		display: flex;
		gap: 4px;
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
		font-size: 0.8125rem;
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
		padding: 10px 12px;
		background: var(--bg);
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
	}
	.upload span {
		flex: 1 1 200px;
	}
	.upload .btn {
		height: 2.25rem;
	}

	.apply {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.state {
		font-size: 0.8125rem;
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
</style>
