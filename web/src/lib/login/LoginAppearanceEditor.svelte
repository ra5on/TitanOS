<script lang="ts">
	// Settings › Appearance › Login page: one look for the whole NAS, only for
	// owners and administrators. Saved on its own, independent of "Apply" for
	// the account's appearance.
	import { onDestroy, onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Slider from '#lib/components/Slider.svelte';
	import EffectSlider from '#lib/dashboard/EffectSlider.svelte';
	import { BACKGROUND_IDS } from '#lib/dashboard/visual.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Login from '#lib/pages/Login.svelte';
	import { session } from '#lib/session.svelte.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { isHex, type Hex } from '#lib/theme/color.ts';
	import {
		ACCENT_IDS,
		ACCENTS,
		LIMITS,
		resolvePalette,
		SURFACE_STYLES,
	} from '#lib/theme/themes.ts';
	import {
		cardContrast,
		defaultLoginLook,
		dimmed,
		isDefaultLook,
		LOGIN_IMAGE_MAX,
		LOGIN_IMAGE_TYPES,
		LOGIN_LOCALES,
		LOGIN_NOTICE_MAX,
		LOGIN_POSITIONS,
		LOGIN_SOURCES,
		LOGIN_THEMES,
		LOGIN_TITLE_MAX,
		LOGIN_WIDTHS,
		loginAppearance,
		loginBackgroundURL,
		loginSaveBody,
		sanitizeLoginLook,
		type LoginLocale,
		type LoginLook,
	} from './look.ts';
	import { loadLoginLook, loginLook, PREVIEW_KEY, useLoginLook } from './state.svelte.ts';

	const initial = sanitizeLoginLook(loginLook.value ?? defaultLoginLook());
	let saved = $state<LoginLook>(structuredClone(initial));
	let draft = $state<LoginLook>(structuredClone(initial));
	let pending = $state<{ file: File; url: string } | null>(null);
	let saving = $state(false);
	let message = $state<{ text: string; warn: boolean } | null>(null);
	let fileInput = $state<HTMLInputElement>();
	let previewOpen = false;
	let frameWidth = $state(0);

	onMount(async () => {
		await loadLoginLook();
		if (!dirty) {
			saved = loginLook.value ?? defaultLoginLook();
			draft = structuredClone($state.snapshot(saved));
		}
	});
	onDestroy(() => {
		if (pending) URL.revokeObjectURL(pending.url);
	});

	const dirty = $derived(pending !== null || JSON.stringify(draft) !== JSON.stringify(saved));
	const effective = $derived(loginAppearance(draft, appearance.current).appearance);
	const palette = $derived(resolvePalette(effective, appearance.systemLight));
	const picture = $derived(loginBackgroundURL(draft, pending?.url));
	const removesImage = $derived(saved.image !== null && draft.background.source !== 'custom');

	// Contrast of the card's text on what lies behind it: for a picture its
	// average color after darkening, otherwise the theme's background.
	let average = $state<Hex | null>(null);
	$effect(() => {
		const url = picture;
		average = null;
		if (!url) return;
		let cancelled = false;
		averageColor(url).then((color) => {
			if (!cancelled) average = color;
		});
		return () => {
			cancelled = true;
		};
	});
	const behind = $derived(
		picture ? (average ? dimmed(average, draft.background.dim) : null) : palette.bg,
	);
	const ratio = $derived(behind ? cardContrast(palette, effective, behind) : 21);

	// Unsaved changes reach an open preview tab right away.
	$effect(() => {
		const data = JSON.stringify({ look: $state.snapshot(draft), imageURL: pending?.url ?? '' });
		if (previewOpen) writePreview(data);
	});

	function writePreview(data: string) {
		try {
			localStorage.setItem(PREVIEW_KEY, data);
		} catch {
			// Without storage the tab shows the saved look.
		}
	}

	function openPreview() {
		previewOpen = true;
		writePreview(JSON.stringify({ look: $state.snapshot(draft), imageURL: pending?.url ?? '' }));
		window.open('/login-preview', '_blank', 'noopener');
	}

	async function averageColor(url: string): Promise<Hex | null> {
		try {
			const image = new Image();
			image.src = url;
			await image.decode();
			const canvas = document.createElement('canvas');
			canvas.width = canvas.height = 16;
			const context = canvas.getContext('2d', { willReadFrequently: true });
			if (!context) return null;
			context.drawImage(image, 0, 0, 16, 16);
			const data = context.getImageData(0, 0, 16, 16).data;
			const sum = [0, 0, 0];
			for (let i = 0; i < data.length; i += 4) for (let c = 0; c < 3; c++) sum[c] += data[i + c];
			const color =
				'#' +
				sum
					.map((v) =>
						Math.round(v / (data.length / 4))
							.toString(16)
							.padStart(2, '0'),
					)
					.join('');
			return isHex(color) ? color.toUpperCase() : null;
		} catch {
			return null;
		}
	}

	function setText(locale: LoginLocale, field: 'title' | 'notice', value: string) {
		const current = draft.texts[locale] ?? { title: '', notice: '' };
		draft.texts = { ...draft.texts, [locale]: { ...current, [field]: value } };
	}

	async function pickImage() {
		const file = fileInput?.files?.[0];
		if (fileInput) fileInput.value = '';
		if (!file) return;
		message = null;
		if (!LOGIN_IMAGE_TYPES.includes(file.type)) {
			message = { text: t('dashboard.visual.imageType'), warn: true };
			return;
		}
		if (file.size > LOGIN_IMAGE_MAX) {
			message = { text: t('loginLook.imageTooLarge'), warn: true };
			return;
		}
		const url = URL.createObjectURL(file);
		try {
			const image = new Image();
			image.src = url;
			await image.decode();
			const hash = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
			const id = [...new Uint8Array(hash)].map((n) => n.toString(16).padStart(2, '0')).join('');
			if (pending) URL.revokeObjectURL(pending.url);
			pending = { file, url };
			// Shown until saving; titand stores its own re-encoded copy.
			draft.image = {
				id,
				width: image.naturalWidth,
				height: image.naturalHeight,
				bytes: file.size,
			};
			draft.background = { ...draft.background, source: 'custom', id: '' };
		} catch {
			URL.revokeObjectURL(url);
			message = { text: t('dashboard.visual.imageFailed'), warn: true };
		}
	}

	async function save() {
		saving = true;
		message = null;
		try {
			let result: unknown = null;
			if (
				!pending &&
				isDefaultLook({ ...draft, image: null }) &&
				(saved.image === null || removesImage)
			) {
				await api.delete('/login-appearance');
			} else if (pending && draft.background.source === 'custom') {
				const form = new FormData();
				form.append('settings', JSON.stringify(loginSaveBody(draft)));
				form.append('image', pending.file, pending.file.name);
				result = await api.put('/login-appearance', form);
			} else {
				result = await api.put('/login-appearance', loginSaveBody(draft, removesImage));
			}
			const next = result ? sanitizeLoginLook(result) : null;
			useLoginLook(next);
			if (pending) URL.revokeObjectURL(pending.url);
			pending = null;
			saved = next ?? defaultLoginLook();
			draft = structuredClone($state.snapshot(saved));
			message = { text: t('loginLook.saved'), warn: false };
		} catch (err) {
			message = { text: apiErrorText(err), warn: true };
		} finally {
			saving = false;
		}
	}

	function reset() {
		if (pending) URL.revokeObjectURL(pending.url);
		pending = null;
		draft = { ...defaultLoginLook(), image: saved.image };
		message = null;
	}

	const format = (n: number) =>
		new Intl.NumberFormat(i18n.locale, { maximumFractionDigits: 1 }).format(n);
	const scale = $derived(frameWidth > 0 ? frameWidth / 1280 : 0.4);
</script>

<section class="tile panel login-look" aria-labelledby="h-login-look">
	<h2 class="tile-title" id="h-login-look">{t('loginLook.title')}</h2>
	<p class="hint muted">{t('loginLook.intro')}</p>

	<div class="frame" bind:clientWidth={frameWidth} aria-label={t('loginLook.preview')} role="img">
		<div class="stage" style:transform={`scale(${scale})`} inert>
			<Login preview look={draft} imageURL={pending?.url ?? ''} hostname={session.data?.hostname} />
		</div>
	</div>
	<div class="row-actions">
		<button type="button" class="btn" onclick={openPreview}>{t('loginLook.openPreview')}</button>
	</div>

	{#if ratio < 4.5}
		<p class="notice-warn" role="status">
			{t('loginLook.contrastWarning', { ratio: format(ratio) + ':1' })}
		</p>
	{/if}

	<div class="divided">
		<label class="stack"
			>{t('loginLook.theme')}<select class="field" bind:value={draft.theme}>
				{#each LOGIN_THEMES as id (id)}
					<option value={id}
						>{id === 'inherit' ? t('loginLook.themeInherit') : t(`theme.${id}`)}</option
					>
				{/each}
			</select></label
		>
		{#if draft.theme === 'inherit'}
			<p class="hint muted">{t('loginLook.themeInheritHint')}</p>
		{:else}
			<div class="accents" role="radiogroup" aria-label={t('appearance.accent')}>
				{#each ACCENT_IDS as id (id)}
					<button
						type="button"
						role="radio"
						aria-label={t(`accent.${id}`)}
						aria-checked={draft.accent === id}
						style:background={ACCENTS[id].hex}
						onclick={() => (draft.accent = id)}
					></button>
				{/each}
				<input
					type="color"
					aria-label={t('appearance.customAccent')}
					class:picked={draft.accent === 'custom'}
					bind:value={draft.customAccent}
					oninput={() => (draft.accent = 'custom')}
				/>
			</div>
			<label class="stack"
				>{t('appearance.style')}<select class="field" bind:value={draft.style}>
					{#each SURFACE_STYLES as id (id)}<option value={id}>{t(`style.${id}`)}</option>{/each}
				</select></label
			>
			{#if draft.style === 'glass'}
				<Slider
					id="login-glass"
					label={t('appearance.transparency')}
					bind:value={draft.glass}
					limit={LIMITS.glass}
					unit="%"
				/>
				<Slider
					id="login-blur"
					label={t('appearance.blur')}
					bind:value={draft.blur}
					limit={LIMITS.blur}
					unit="px"
				/>
				<p class="hint muted">{t('loginLook.glassHint')}</p>
			{/if}
		{/if}
	</div>

	<div class="divided">
		<label class="stack"
			>{t('loginLook.background')}<select
				class="field"
				value={draft.background.source}
				onchange={(e) => {
					const source = e.currentTarget.value as LoginLook['background']['source'];
					draft.background = {
						...draft.background,
						source,
						id: source === 'builtin' ? draft.background.id || BACKGROUND_IDS[0] : '',
					};
					if (source === 'custom' && !draft.image) fileInput?.click();
				}}
			>
				{#each LOGIN_SOURCES as id (id)}<option value={id}>{t(`loginLook.bg.${id}`)}</option>{/each}
			</select></label
		>
		{#if draft.background.source === 'builtin'}
			<div class="motifs" role="radiogroup" aria-label={t('loginLook.bg.builtin')}>
				{#each BACKGROUND_IDS as id (id)}
					<button
						type="button"
						role="radio"
						aria-checked={draft.background.id === id}
						aria-label={t('dashboard.visual.pick', { name: t(`dashboard.bg.${id}` as MessageKey) })}
						onclick={() => (draft.background = { ...draft.background, id })}
						><img src={'/backgrounds/' + id + '.webp'} alt="" loading="lazy" /><span
							>{t(`dashboard.bg.${id}` as MessageKey)}</span
						></button
					>
				{/each}
			</div>
		{/if}
		{#if draft.background.source === 'custom'}
			<div class="upload">
				<span class="muted"
					>{#if pending}{pending.file.name} · {t(
							'loginLook.imagePending',
						)}{:else if draft.image}{draft.image.width} × {draft.image.height}{:else}{t(
							'loginLook.imageNone',
						)}{/if}</span
				>
				<button type="button" class="btn" onclick={() => fileInput?.click()}
					>{draft.image ? t('loginLook.replaceImage') : t('loginLook.chooseImage')}</button
				>
			</div>
			<p class="hint muted">{t('loginLook.imageHint')}</p>
		{/if}
		<input
			bind:this={fileInput}
			type="file"
			accept={LOGIN_IMAGE_TYPES.join(',')}
			hidden
			onchange={pickImage}
		/>
		{#if removesImage}<p class="hint muted">{t('loginLook.imageRemoved')}</p>{/if}
		{#if draft.background.source !== 'theme'}
			<label class="stack"
				>{t('dashboard.visual.fit')}<select class="field" bind:value={draft.background.fit}>
					<option value="cover">{t('dashboard.visual.cover')}</option>
					<option value="contain">{t('dashboard.visual.contain')}</option>
				</select></label
			>
			{#if draft.background.fit === 'cover'}
				<EffectSlider
					label={t('dashboard.visual.zoom')}
					value={draft.background.zoom}
					min={100}
					max={200}
					onbegin={() => {}}
					onchange={(v) => (draft.background.zoom = v)}
				/>
				<EffectSlider
					label={t('dashboard.visual.positionX')}
					value={draft.background.x}
					onbegin={() => {}}
					onchange={(v) => (draft.background.x = v)}
				/>
				<EffectSlider
					label={t('dashboard.visual.positionY')}
					value={draft.background.y}
					onbegin={() => {}}
					onchange={(v) => (draft.background.y = v)}
				/>
			{/if}
			<EffectSlider
				label={t('dashboard.visual.dim')}
				value={draft.background.dim}
				max={80}
				onbegin={() => {}}
				onchange={(v) => (draft.background.dim = v)}
			/>
		{/if}
	</div>

	<div class="divided two">
		<label class="stack"
			>{t('loginLook.position')}<select class="field" bind:value={draft.position}>
				{#each LOGIN_POSITIONS as id (id)}<option value={id}>{t(`loginLook.position.${id}`)}</option
					>{/each}
			</select></label
		>
		<label class="stack"
			>{t('loginLook.width')}<select class="field" bind:value={draft.width}>
				{#each LOGIN_WIDTHS as id (id)}<option value={id}>{t(`loginLook.width.${id}`)}</option
					>{/each}
			</select></label
		>
	</div>
	<p class="hint muted">{t('loginLook.positionHint')}</p>
	<label class="check"
		><input
			type="checkbox"
			checked={!draft.hideHost}
			onchange={(e) => (draft.hideHost = !e.currentTarget.checked)}
		/>{t('loginLook.showHost')}</label
	>
	<label class="check"
		><input
			type="checkbox"
			checked={!draft.hideLogo}
			onchange={(e) => (draft.hideLogo = !e.currentTarget.checked)}
		/>{t('loginLook.showLogo')}</label
	>
	<label class="check"
		><input type="checkbox" bind:checked={draft.clock} />{t('loginLook.clock')}</label
	>

	<fieldset class="divided texts">
		<legend class="tile-title">{t('loginLook.texts')}</legend>
		{#each LOGIN_LOCALES as locale (locale)}
			{@const text = draft.texts[locale] ?? { title: '', notice: '' }}
			<div class="language">
				<span class="lang">{t(`lang.${locale}`)}</span>
				<label class="stack"
					>{t('loginLook.textTitle')}<input
						class="field"
						type="text"
						lang={locale}
						maxlength={LOGIN_TITLE_MAX}
						placeholder={locale === 'de' ? 'Anmelden' : 'Sign in'}
						value={text.title}
						oninput={(e) => setText(locale, 'title', e.currentTarget.value)}
					/></label
				>
				<label class="stack"
					>{t('loginLook.textNotice')}<input
						class="field"
						type="text"
						lang={locale}
						maxlength={LOGIN_NOTICE_MAX}
						value={text.notice}
						oninput={(e) => setText(locale, 'notice', e.currentTarget.value)}
					/><small class="muted"
						>{t('loginLook.count', {
							count: [...text.notice].length,
							max: LOGIN_NOTICE_MAX,
						})}</small
					></label
				>
			</div>
		{/each}
		<p class="hint muted">{t('loginLook.textsHint')}</p>
	</fieldset>

	<div class="row-actions">
		<button type="button" class="btn btn-primary" onclick={save} disabled={saving || !dirty}
			>{t('loginLook.save')}</button
		>
		<button type="button" class="btn" onclick={reset} disabled={saving}
			>{t('loginLook.reset')}</button
		>
		<span class="state" class:warn={message?.warn} role="status">
			{#if message}{message.text}{:else if dirty}{t('loginLook.unsaved')}{/if}
		</span>
	</div>
</section>

<style>
	.hint {
		font-size: var(--text-sm);
	}
	.frame {
		position: relative;
		width: 100%;
		aspect-ratio: 16 / 10;
		overflow: hidden;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
	}
	.stage {
		position: absolute;
		top: 0;
		left: 0;
		width: 1280px;
		height: 800px;
		transform-origin: top left;
		pointer-events: none;
	}
	.accents {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.accents button,
	.accents input {
		width: 36px;
		height: 36px;
		padding: 0;
		border: 3px solid transparent;
		border-radius: 50%;
		cursor: pointer;
		background-clip: padding-box;
	}
	.accents button[aria-checked='true'],
	.accents input.picked {
		border-color: var(--text);
	}
	.motifs {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
		gap: 8px;
	}
	.motifs button {
		padding: 0;
		overflow: hidden;
		border: 2px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: left;
		cursor: pointer;
	}
	.motifs button[aria-checked='true'] {
		border-color: var(--accent);
	}
	.motifs img {
		display: block;
		width: 100%;
		aspect-ratio: 16 / 10;
		object-fit: cover;
	}
	.motifs span {
		display: block;
		padding: 4px 6px;
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
		font-size: var(--text-base);
	}
	.upload span {
		flex: 1 1 180px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.two {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	.texts {
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	.texts legend {
		padding: 0;
		margin-bottom: 8px;
	}
	.language {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
		gap: 6px 12px;
		margin-bottom: 12px;
	}
	.lang {
		grid-column: 1 / -1;
		font-size: var(--text-base);
		font-weight: 600;
	}
	.language small {
		font-size: var(--text-xs);
	}
	.state {
		font-size: var(--text-base);
		color: var(--muted);
	}
	.state.warn {
		color: var(--warn-text);
	}
	@media (max-width: 760px) {
		.two,
		.language {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
