<script lang="ts">
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { resolveAccent, resolvePalette } from '#lib/theme/themes.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import EffectSlider from './EffectSlider.svelte';
	import {
		BACKGROUND_IDS,
		MAX_IMAGES,
		backgroundURL,
		type DashboardVisual,
		type PersonalImage,
	} from './visual.ts';
	let {
		value,
		urls,
		onbegin,
		onchange,
		onpreview,
		onupload,
		onclose,
	}: {
		value: DashboardVisual;
		urls: Record<string, string>;
		onbegin: () => void;
		onchange: (v: DashboardVisual) => void;
		onpreview: (v: DashboardVisual | null) => void;
		onupload: (id: string, file: File, url: string) => void;
		onclose: () => void;
	} = $props();
	let tab = $state<'surface' | 'background' | 'motion'>('surface');
	let gallery = $state<DashboardVisual | null>(null),
		source = $state<'builtin' | 'own'>('builtin');
	let uploadError = $state(''),
		uploading = $state(false),
		serial = 0;
	let input = $state<HTMLInputElement>();
	let dragging = $state(false);
	const name = (v: DashboardVisual) =>
		v.background.source === 'builtin'
			? t(`dashboard.bg.${v.background.id}` as MessageKey)
			: v.background.source === 'custom'
				? v.images.find((i) => i.id === v.background.id)?.name || t('dashboard.visual.own')
				: t(`dashboard.visual.${v.background.source}` as MessageKey);
	const thumb = (v: DashboardVisual) =>
		backgroundURL(v, urls) ||
		(v.background.source === 'inherit' &&
		appearance.current.bg === 'image' &&
		appearance.current.bgVersion
			? '/api/v1/background?v=' + appearance.current.bgVersion
			: '');
	function set<K extends keyof DashboardVisual>(key: K, next: DashboardVisual[K]) {
		onchange({ ...value, [key]: next });
	}
	function background(next: Partial<DashboardVisual['background']>) {
		onbegin();
		onchange({ ...value, background: { ...value.background, ...next } });
	}
	function open() {
		serial++;
		gallery = structuredClone($state.snapshot(value));
		source = value.background.source === 'custom' ? 'own' : 'builtin';
		uploadError = '';
	}
	function preview(next: DashboardVisual) {
		gallery = next;
		onpreview(next);
	}
	function cancel() {
		serial++;
		gallery = null;
		uploadError = '';
		onpreview(null);
	}
	function apply() {
		if (!gallery) return;
		onbegin();
		onchange(gallery);
		gallery = null;
		onpreview(null);
		serial++;
	}
	function choose(kind: 'builtin' | 'custom', id: string) {
		if (gallery) preview({ ...gallery, background: { ...gallery.background, source: kind, id } });
	}
	function remove(id: string) {
		if (!gallery) return;
		preview({
			...gallery,
			images: gallery.images.filter((i) => i.id !== id),
			background:
				gallery.background.id === id
					? { ...gallery.background, source: 'inherit', id: '' }
					: gallery.background,
		});
	}
	async function upload(file?: File) {
		if (!file || !gallery) return;
		uploadError = '';
		if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
			uploadError = t('dashboard.visual.imageType');
			return;
		}
		if (file.size > 10 * 1024 * 1024) {
			uploadError = t('error.too_large');
			return;
		}
		const current = serial;
		uploading = true;
		let url = '';
		try {
			const bytes = await file.arrayBuffer();
			const hash = await crypto.subtle.digest('SHA-256', bytes);
			const id = [...new Uint8Array(hash)].map((n) => n.toString(16).padStart(2, '0')).join('');
			url = URL.createObjectURL(file);
			const image = new Image();
			image.src = url;
			await image.decode();
			if (!gallery || serial !== current) {
				URL.revokeObjectURL(url);
				return;
			}
			if (!gallery.images.some((i) => i.id === id) && gallery.images.length >= MAX_IMAGES) {
				URL.revokeObjectURL(url);
				uploadError = t('dashboard.visual.imageLimit', { count: MAX_IMAGES });
				return;
			}
			const meta: PersonalImage = {
				id,
				name: file.name,
				width: image.naturalWidth,
				height: image.naturalHeight,
				bytes: file.size,
			};
			onupload(id, file, url);
			preview({
				...gallery,
				images: gallery.images.some((i) => i.id === id)
					? gallery.images
					: [...gallery.images, meta],
				background: { ...gallery.background, source: 'custom', id },
			});
		} catch {
			if (url) URL.revokeObjectURL(url);
			uploadError = t('dashboard.visual.imageFailed');
		} finally {
			uploading = false;
		}
	}
</script>

<aside class="effects-panel tile" aria-label={t('dashboard.visual.all')}>
	<div class="panel-head">
		<div>
			<div class="effects-eyebrow">{t('dashboard.visual.all')}</div>
			<h2>{t('dashboard.visual.title')}</h2>
			<p>{t('dashboard.visual.intro')}</p>
		</div>
		<button class="icon-button" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={17} /></button
		>
	</div>
	<div class="effects-tabs" role="tablist" aria-label={t('dashboard.visual.title')}>
		{#each ['surface', 'background', 'motion'] as id}<button
				role="tab"
				aria-selected={tab === id}
				onclick={() => (tab = id as typeof tab)}>{t(`dashboard.visual.${id}` as MessageKey)}</button
			>{/each}
	</div>
	{#if tab === 'surface'}
		<div class="effect-heading">
			{t('dashboard.visual.surfaces')}<button
				onclick={() => {
					onbegin();
					onchange({ ...value, transparency: 48, blur: 14, glow: 18, glowColor: '' });
				}}>{t('dashboard.visual.reset')}</button
			>
		</div>
		<EffectSlider
			label={t('dashboard.visual.transparency')}
			value={value.transparency}
			max={90}
			hint={t('dashboard.visual.transparencyHint')}
			{onbegin}
			onchange={(v) => set('transparency', v)}
		/>
		<EffectSlider
			label={t('dashboard.visual.blur')}
			value={value.blur}
			max={40}
			unit="px"
			hint={t('dashboard.visual.blurHint')}
			{onbegin}
			onchange={(v) => set('blur', v)}
		/>
		<EffectSlider
			label={t('dashboard.visual.glow')}
			value={value.glow}
			hint={t('dashboard.visual.glowHint')}
			{onbegin}
			onchange={(v) => set('glow', v)}
		/>
		<label class="color-choice"
			><input
				type="color"
				aria-label={t('dashboard.visual.glowColor')}
				value={value.glowColor ||
					resolveAccent(
						appearance.current,
						resolvePalette(appearance.current, appearance.systemLight),
					)}
				onfocus={onbegin}
				oninput={(e) => set('glowColor', e.currentTarget.value)}
			/>{t('dashboard.visual.glowColor')}</label
		>
		{#if value.transparency > 65}<p class="hint">{t('dashboard.visual.contrastHint')}</p>{/if}
		<hr />
		<div class="effect-heading">{t('dashboard.visual.background')}</div>
	{/if}
	{#if tab !== 'motion'}
		<div class="selected-wallpaper">
			{#if thumb(value)}<img src={thumb(value)} alt={name(value)} />{:else}<span class="no-image"
					><Icon name="image" size={28} /></span
				>{/if}
			<div>
				<strong>{name(value)}</strong><small
					>{t(
						value.background.source === 'custom'
							? 'dashboard.visual.own'
							: 'dashboard.visual.local',
					)}</small
				>
			</div>
		</div>
		<button class="btn wide-btn" onclick={open}>{t('dashboard.visual.choose')}</button>
		{#if tab === 'background'}
			<label class="effect-field"
				>{t('dashboard.visual.fit')}<select
					class="field"
					value={value.background.fit}
					onchange={(e) =>
						background({ fit: e.currentTarget.value as 'contain' | 'cover', zoom: 100 })}
					><option value="contain">{t('dashboard.visual.contain')}</option><option value="cover"
						>{t('dashboard.visual.cover')}</option
					></select
				></label
			>
			{#if value.background.fit === 'cover'}
				<EffectSlider
					label={t('dashboard.visual.zoom')}
					value={value.background.zoom}
					min={100}
					max={200}
					{onbegin}
					onchange={(v) => onchange({ ...value, background: { ...value.background, zoom: v } })}
				/>
				<EffectSlider
					label={t('dashboard.visual.positionX')}
					value={value.background.x}
					{onbegin}
					onchange={(v) => onchange({ ...value, background: { ...value.background, x: v } })}
				/>
				<EffectSlider
					label={t('dashboard.visual.positionY')}
					value={value.background.y}
					{onbegin}
					onchange={(v) => onchange({ ...value, background: { ...value.background, y: v } })}
				/>
			{/if}
		{/if}
		<EffectSlider
			label={t('dashboard.visual.dim')}
			value={value.dim}
			max={80}
			{onbegin}
			onchange={(v) => set('dim', v)}
		/>
		{#if tab === 'background'}<label class="effects-check"
				><input
					type="checkbox"
					checked={value.background.fixed}
					onchange={(e) => background({ fixed: e.currentTarget.checked })}
				/>{t('dashboard.visual.fixed')}</label
			>
			<div class="background-modes">
				<button class="link" onclick={() => background({ source: 'inherit', id: '' })}
					>{t('dashboard.visual.inherit')}</button
				><button class="link" onclick={() => background({ source: 'none', id: '' })}
					>{t('dashboard.visual.none')}</button
				>
			</div>
			<p class="hint">{t('dashboard.visual.containHint')}</p>{:else}<p class="hint">
				{t('dashboard.visual.globalHint')}
			</p>{/if}
	{:else}
		<div class="effect-heading">{t('dashboard.visual.metrics')}</div>
		<label class="effects-check"
			><input
				type="checkbox"
				checked={value.motion}
				onchange={(e) => {
					onbegin();
					set('motion', e.currentTarget.checked);
				}}
			/>{t('dashboard.visual.smooth')}</label
		>
		<EffectSlider
			label={t('dashboard.visual.duration')}
			value={value.duration}
			min={150}
			max={1000}
			step={50}
			unit="ms"
			hint={t('dashboard.visual.durationHint')}
			{onbegin}
			onchange={(v) => set('duration', v)}
		/>
		<p class="hint">{t('dashboard.visual.motionHint')}</p>
	{/if}
	<p class="effects-footer">{t('dashboard.editor.preview')}</p>
</aside>
{#if gallery}<Dialog
		title={t('dashboard.visual.gallery')}
		wide
		maxWidth={1210}
		locked={uploading}
		onclose={cancel}
	>
		<div class="gallery-head">
			<p>{t('dashboard.visual.galleryHint')}</p>
			<button class="btn" onclick={() => (source = 'own')}
				><Icon name="plus" size={16} />{t('dashboard.visual.add')}</button
			>
		</div>
		<div class="source-tabs" role="tablist" aria-label={t('dashboard.visual.gallery')}>
			<button role="tab" aria-selected={source === 'builtin'} onclick={() => (source = 'builtin')}
				>{t('dashboard.visual.builtin')} <span>10</span></button
			><button role="tab" aria-selected={source === 'own'} onclick={() => (source = 'own')}
				>{t('dashboard.visual.own')} <span>{gallery.images.length}</span></button
			>
		</div>
		<div class="gallery-content">
			<div>
				{#if source === 'builtin'}<div class="wallpaper-grid">
						{#each BACKGROUND_IDS as id}<button
								class="wallpaper-choice"
								aria-label={t('dashboard.visual.pick', {
									name: t(`dashboard.bg.${id}` as MessageKey),
								})}
								aria-pressed={gallery.background.source === 'builtin' &&
									gallery.background.id === id}
								onclick={() => choose('builtin', id)}
								><img src={'/backgrounds/' + id + '.webp'} alt="" loading="lazy" /><span
									class="chosen">✓</span
								><span class="wallpaper-label">{t(`dashboard.bg.${id}` as MessageKey)}</span
								></button
							>{/each}
					</div>
				{:else}
					<div
						class="drop-zone"
						class:dragging
						role="region"
						aria-label={t('dashboard.visual.drop')}
						ondragover={(e) => {
							e.preventDefault();
							dragging = true;
						}}
						ondragleave={() => (dragging = false)}
						ondrop={(e) => {
							e.preventDefault();
							dragging = false;
							void upload(e.dataTransfer?.files[0]);
						}}
					>
						<Icon name="upload" size={36} />
						<h3>{t('dashboard.visual.drop')}</h3>
						<p>{t('dashboard.visual.orChoose')}</p>
						<button class="btn" disabled={uploading} onclick={() => input?.click()}
							>{t('dashboard.visual.file')}</button
						>
						<p>{t('dashboard.visual.formats')}</p>
						<input
							bind:this={input}
							type="file"
							accept="image/jpeg,image/png,image/webp"
							hidden
							onchange={(e) => {
								void upload(e.currentTarget.files?.[0]);
								e.currentTarget.value = '';
							}}
						/>
					</div>
					{#if uploading}<p role="status">{t('common.loading')}</p>{/if}
					{#if uploadError}<p class="notice-error" role="alert">{uploadError}</p>{/if}
					<div class="personal-images">
						{#each gallery.images as image}<div class="personal-image">
								<button
									class="personal-pick"
									aria-pressed={gallery.background.source === 'custom' &&
										gallery.background.id === image.id}
									onclick={() => choose('custom', image.id)}
									><img src={urls[image.id] || '/api/v1/background/' + image.id} alt="" /><span
										><strong>{image.name}</strong><small
											>{image.width} × {image.height} · {formatBytes(image.bytes)}</small
										></span
									></button
								><button
									class="icon-button"
									aria-label={t('dashboard.visual.remove', { name: image.name })}
									onclick={() => remove(image.id)}><Icon name="close" size={16} /></button
								>
							</div>{/each}
					</div>
				{/if}
			</div>
			<aside class="gallery-preview">
				{#if thumb(gallery)}<img src={thumb(gallery)} alt={name(gallery)} />{/if}
				<h3>{name(gallery)}</h3>
				<p>{t('dashboard.visual.selectionHint')}</p>
				<small
					>{t(
						gallery.background.source === 'custom'
							? 'dashboard.visual.own'
							: 'dashboard.visual.local',
					)}</small
				><label class="effect-field"
					>{t('dashboard.visual.fit')}<select
						class="field"
						value={gallery.background.fit}
						onchange={(e) => {
							if (gallery)
								preview({
									...gallery,
									background: {
										...gallery.background,
										fit: e.currentTarget.value as 'contain' | 'cover',
										zoom: 100,
									},
								});
						}}
						><option value="contain">{t('dashboard.visual.contain')}</option><option value="cover"
							>{t('dashboard.visual.cover')}</option
						></select
					></label
				>
				<p>{t('dashboard.visual.containHint')}</p>
			</aside>
		</div>
		<div class="dialog-actions">
			<span class="save-hint">{t('dashboard.visual.saveHint')}</span><button
				class="btn"
				disabled={uploading}
				onclick={cancel}>{t('common.cancel')}</button
			><button class="btn btn-primary" disabled={uploading} onclick={apply}
				>{t('dashboard.visual.use')}</button
			>
		</div>
	</Dialog>{/if}

<style>
	.effects-panel {
		position: sticky;
		top: var(--dashboard-sticky-top, 76px);
		max-height: calc(100vh - var(--dashboard-sticky-top, 76px) - 19px);
		overflow: auto;
		box-sizing: border-box;
		width: 340px;
		flex: none;
		padding: 20px;
		min-width: 0;
		background: var(--surface);
		align-self: flex-start;
	}
	.panel-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 8px;
	}
	h2 {
		margin: 4px 0 5px;
		font-size: 20px;
	}
	p {
		color: var(--muted);
		line-height: 1.5;
		font-size: 12px;
		margin: 0;
	}
	.effects-eyebrow {
		color: var(--accent);
		letter-spacing: 0.08em;
		font-size: 10px;
		font-weight: 600;
	}
	.effects-tabs {
		display: flex;
		border-bottom: 1px solid var(--border);
		gap: 17px;
		margin: 18px 0;
	}
	.effects-tabs button,
	.source-tabs button {
		font: inherit;
		font-size: 12px;
		background: none;
		border: 0;
		color: var(--muted);
		padding: 0 0 11px;
		cursor: pointer;
		border-bottom: 2px solid transparent;
	}
	.effects-tabs button[aria-selected='true'],
	.source-tabs button[aria-selected='true'] {
		color: var(--text);
		border-bottom-color: var(--accent);
	}
	.effect-heading {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin: 21px 0 16px;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--text);
	}
	.effect-heading button {
		font: inherit;
		font-size: 11px;
		color: var(--accent);
		border: 0;
		background: none;
		cursor: pointer;
	}
	hr {
		border: 0;
		border-top: 1px solid var(--border);
		margin: 22px 0;
	}
	.selected-wallpaper {
		display: flex;
		gap: 12px;
		align-items: center;
		margin: 15px 0;
	}
	.selected-wallpaper img,
	.no-image {
		width: 100px;
		aspect-ratio: 16/9;
		object-fit: contain;
		border-radius: var(--radius-small);
		background: var(--bg);
		border: 1px solid var(--border);
		flex: none;
	}
	.no-image {
		display: grid;
		place-items: center;
	}
	.selected-wallpaper strong {
		display: block;
		font-size: 13px;
		margin-bottom: 5px;
		overflow-wrap: anywhere;
	}
	.selected-wallpaper small {
		color: var(--muted);
		font-size: 11px;
	}
	.wide-btn {
		width: 100%;
		justify-content: center;
		margin: 0 0 16px;
	}
	.effect-field {
		display: block;
		margin: 13px 0;
		font-size: 12px;
		color: var(--text);
	}
	.effect-field select {
		margin-top: 8px;
		width: 100%;
	}
	.hint {
		padding: 12px;
		border-radius: var(--radius-small);
		background: var(--chip);
		border: 1px solid var(--border);
		font-size: 11px;
	}
	.effects-footer {
		font-size: 11px;
		margin-top: 18px;
		padding-top: 14px;
		border-top: 1px solid var(--border);
	}
	.effects-check {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		margin: 16px 0;
	}
	.effects-check input {
		accent-color: var(--accent);
	}
	.color-choice {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 11px;
		color: var(--muted);
		margin-bottom: 18px;
	}
	.color-choice input {
		width: 25px;
		height: 25px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: none;
		padding: 2px;
	}
	.background-modes {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		margin-bottom: 15px;
	}
	.gallery-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
	}
	.source-tabs {
		display: flex;
		gap: 24px;
		border-bottom: 1px solid var(--border);
	}
	.source-tabs button {
		padding: 10px 0 13px;
	}
	.source-tabs span {
		color: var(--accent);
		background: var(--chip);
		padding: 2px 6px;
		border-radius: var(--radius-small);
		margin-left: 5px;
		font-size: 10px;
	}
	.gallery-content {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 245px;
		gap: 24px;
		padding: 8px 0;
	}
	.wallpaper-grid {
		display: grid;
		grid-template-columns: repeat(5, minmax(0, 1fr));
		gap: 17px 13px;
		align-content: start;
	}
	.wallpaper-choice {
		padding: 0;
		text-align: left;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		overflow: hidden;
		background: var(--chip);
		color: var(--text);
		cursor: pointer;
		font: inherit;
		position: relative;
	}
	.wallpaper-choice img {
		display: block;
		width: 100%;
		aspect-ratio: 16/10;
		object-fit: contain;
		background: var(--bg);
	}
	.wallpaper-label {
		display: block;
		padding: 9px 10px;
		font-size: 12px;
	}
	.wallpaper-choice[aria-pressed='true'] {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.chosen {
		position: absolute;
		right: 7px;
		top: 7px;
		background: var(--accent);
		color: var(--on-accent);
		border-radius: 50%;
		width: 21px;
		height: 21px;
		align-items: center;
		justify-content: center;
		font-size: 13px;
		display: none;
	}
	.wallpaper-choice[aria-pressed='true'] .chosen {
		display: flex;
	}
	.gallery-preview {
		border-left: 1px solid var(--border);
		padding-left: 23px;
	}
	.gallery-preview img {
		width: 100%;
		aspect-ratio: 16/9;
		object-fit: contain;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--bg);
	}
	h3 {
		font-size: 17px;
		margin: 13px 0 6px;
	}
	.gallery-preview p {
		margin: 0 0 15px;
	}
	.gallery-preview small,
	.save-hint {
		font-size: 11px;
		color: var(--muted);
	}
	.save-hint {
		margin-right: auto;
		align-self: center;
	}
	.drop-zone {
		min-height: 210px;
		border: 1px dashed var(--accent);
		border-radius: var(--radius-tile);
		background: var(--bg);
		display: flex;
		align-items: center;
		justify-content: center;
		text-align: center;
		padding: 22px;
		flex-direction: column;
		gap: 12px;
	}
	.drop-zone.dragging {
		background: var(--chip);
	}
	.drop-zone h3 {
		margin: 0;
	}
	.personal-images {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin-top: 17px;
	}
	.personal-image {
		display: flex;
		align-items: center;
		gap: 14px;
		background: var(--chip);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		padding: 10px;
	}
	.personal-pick {
		display: flex;
		align-items: center;
		gap: 14px;
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: inherit;
		text-align: left;
		font: inherit;
		cursor: pointer;
	}
	.personal-pick[aria-pressed='true'] {
		outline: 1px solid var(--accent);
		outline-offset: 4px;
	}
	.personal-image img {
		width: 96px;
		aspect-ratio: 16/9;
		object-fit: contain;
		border-radius: var(--radius-small);
	}
	.personal-image strong {
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.personal-image small {
		display: block;
		font-size: 11px;
		color: var(--muted);
		margin: 5px 0;
	}
	.icon-button {
		background: none;
		border: 0;
		color: var(--muted);
		padding: 4px;
		cursor: pointer;
	}
	button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 3px;
	}
	@media (max-width: 1100px) {
		.wallpaper-grid {
			grid-template-columns: repeat(3, minmax(0, 1fr));
		}
		.effects-panel {
			width: 300px;
		}
	}
	@media (max-width: 760px) {
		.effects-panel {
			position: static;
			max-height: none;
			width: 100%;
			max-width: none;
			box-sizing: border-box;
		}
		.gallery-content {
			grid-template-columns: 1fr;
		}
		.gallery-preview {
			border-left: 0;
			border-top: 1px solid var(--border);
			padding: 18px 0 0;
		}
		.wallpaper-grid {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.gallery-head {
			align-items: flex-start;
		}
		.personal-image img {
			width: 64px;
		}
	}
</style>
