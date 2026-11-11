<script lang="ts">
	// PLAN 63: the remote view ("Fernansicht") of a Docker app. TitanOS opens
	// the app in a browser on the NAS; this view shows its picture (WebCodecs
	// video on a canvas), plays its sound and sends mouse, keys, text, the
	// clipboard and uploads back (protocol: docs/FERNANSICHT.md, remote.ts).
	import type { Snippet } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { formatBytes } from '../format.ts';
	import { t } from '../i18n/index.svelte.ts';
	import RemoteControls from './RemoteControls.svelte';
	import { RemoteSession } from './remote-session.svelte.ts';
	import RemoteVolume from './RemoteVolume.svelte';

	let {
		app,
		title,
		phone = false,
		onopentab,
		onback,
		inTitle = false,
		menuHost = null,
		splash,
	}: {
		/** The Docker app (compose project) as the NAS names it. */
		app: string;
		title: string;
		/** Full screen on the phone with its own bar (draft 03). */
		phone?: boolean;
		onopentab: () => void;
		onback?: () => void;
		/** PLAN 66: in a window the controls live in the window's ⋯ menu, not in a bar. */
		inTitle?: boolean;
		/** The open ⋯ menu of the window the controls are put into. */
		menuHost?: HTMLElement | null;
		/** PLAN 66/72: the app's symbol for the start picture. */
		splash?: Snippet;
	} = $props();

	// The connection, picture, sound and input (remote-session.svelte.ts).
	const s = new RemoteSession(() => app);
	let dropping = $state(false);
	let dropNote = $state(false);
	let showVolume = $state(false);

	/** PLAN 67-1: what the menu bar of the window does with the remote view. */
	export const menu = s.menu;
</script>

<div
	class="remote"
	class:phone
	role="presentation"
	ondragover={(e) => {
		// PLAN 66 draft 04: files dragged into the window are uploaded to the app.
		if (s.live && e.dataTransfer?.types.includes('Files')) {
			e.preventDefault();
			dropping = true;
		}
	}}
	ondragleave={(e) => {
		if (!e.currentTarget.contains(e.relatedTarget as Node)) dropping = false;
	}}
	ondrop={(e) => {
		if (!e.dataTransfer?.files.length) return;
		e.preventDefault();
		dropping = false;
		void s.upload(e.dataTransfer.files);
		dropNote = true;
		setTimeout(() => (dropNote = false), 4000);
	}}
>
	{#if dropping}<div class="drop-hint" aria-hidden="true">{t('viewer.dropHint')}</div>{/if}
	{#if dropNote}<p class="drop-note" role="status">{t('viewer.uploadHint')}</p>{/if}
	<RemoteControls {s} {title} {phone} {onopentab} {onback} {inTitle} {menuHost} />
	<input
		type="file"
		multiple
		hidden
		bind:this={s.fileInput}
		onchange={(e) => s.upload(e.currentTarget.files)}
	/>
	<div class="screen" bind:this={s.screen}>
		{#if s.showStats && s.live && s.statsText}<p class="stats-line" role="status">
				{s.statsText}
			</p>{/if}
		<canvas
			bind:this={s.canvas}
			class:hidden={!s.hello || s.phase !== 'live'}
			style:left="{s.shown.left}px"
			style:top="{s.shown.top}px"
			style:width="{s.shown.width}px"
			style:height="{s.shown.height}px"
			style:image-rendering={s.crisp ? 'pixelated' : undefined}
			style:cursor={s.cursor}
			tabindex="0"
			aria-label={t('viewer.screen', { app: title })}
			onpointerdown={s.pointerDown}
			onpointermove={s.pointerMove}
			onpointerup={s.pointerUp}
			onpointercancel={s.pointerUp}
			onwheel={s.wheel}
			onkeydown={s.keyDown}
			onkeyup={s.keyUp}
			onpaste={s.paste}
			onblur={() => s.releaseInput()}
			oncontextmenu={(e) => e.preventDefault()}
		></canvas>
		<input
			class="ime"
			bind:this={s.ime}
			aria-label={t('viewer.keyboardField')}
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			onbeforeinput={s.imeBefore}
			oncompositionend={s.imeComposed}
			onkeydown={s.imeKey}
			oninput={(e) => {
				if (!(e as unknown as InputEvent).isComposing) e.currentTarget.value = s.SENTINEL;
			}}
		/>
		{#if s.phase === 'connecting' || (s.phase === 'live' && !s.painted)}
			{#if splash}
				<!-- PLAN 66 draft 01: the app's symbol, its name and a bar. -->
				<div class="note start" role="status">
					{@render splash()}
					<h3>{title}</h3>
					<span class="start-bar" aria-hidden="true"><span></span></span>
					<p class="muted small">{t('viewer.starting')}</p>
				</div>
			{:else}
				<div class="note" role="status">
					<span class="spinner" aria-hidden="true"></span>
					<p>{t('viewer.starting')}</p>
				</div>
			{/if}
		{:else if s.phase === 'image'}
			<div class="note" role="status">
				<Icon name="download" size={36} />
				<h3>{t('viewer.image.title')}</h3>
				<p>{t('viewer.image.text')}</p>
				{#if s.image?.state === 'error'}
					<p class="error-text">{t('viewer.image.failed', { error: s.image.error })}</p>
					<button type="button" class="btn" onclick={s.reconnect}>{t('common.retry')}</button>
				{:else if s.image && s.image.size > 0}
					<progress max={s.image.size} value={s.image.bytes}></progress>
					<p class="muted small">
						{t('viewer.image.progress', {
							done: formatBytes(s.image.bytes),
							total: formatBytes(s.image.size),
						})}
					</p>
				{:else}<span class="spinner" aria-hidden="true"></span>{/if}
			</div>
		{:else if s.phase !== 'live'}
			<div class="note" role="alert">
				<Icon name="monitor" size={36} />
				{#if s.phase === 'memory'}
					<h3>{t('viewer.memory.title')}</h3>
					<p>{t('viewer.memory.text')}</p>
				{:else if s.phase === 'hostnet'}
					<h3>{t('viewer.hostnet.title')}</h3>
					<p>{t('viewer.hostnet.text')}</p>
				{:else if s.phase === 'unsupported' || s.phase === 'insecure'}
					<h3>{t('viewer.unsupported.title')}</h3>
					<p>{t(s.phase === 'insecure' ? 'viewer.insecure.text' : 'viewer.unsupported.text')}</p>
				{:else if s.phase === 'codec'}
					<h3>{t('viewer.unsupported.title')}</h3>
					<p>{t('viewer.codec.text', { codec: s.hello?.codec ?? '' })}</p>
				{:else}
					<h3>{t('viewer.ended.title')}</h3>
					<p>{t(s.endKey)}</p>
					{#if s.endDetail}<p class="end-detail">{s.endDetail}</p>{/if}
				{/if}
				<div class="actions">
					{#if s.phase === 'ended' || s.phase === 'memory'}
						<button type="button" class="btn btn-primary" onclick={s.reconnect}
							>{t('viewer.reconnect')}</button
						>
					{/if}
					<button type="button" class="btn" onclick={onopentab}
						>{t('desktop.window.openTab')}</button
					>
				</div>
			</div>
		{/if}
	</div>
	{#if phone && s.live}
		<div class="chips">
			{#if s.hello?.audio && !s.noAudio}
				<button
					type="button"
					class="chip-btn"
					aria-expanded={showVolume}
					onclick={() => {
						s.startAudio();
						showVolume = !showVolume;
					}}>{t('viewer.volumeValue', { value: s.volumeText })}</button
				>
			{/if}
			<button type="button" class="chip-btn" onclick={s.openKeyboard}>{t('viewer.keyboard')}</button
			>
			{#if showVolume}
				<div class="volume-sheet"><RemoteVolume {s} /></div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.remote {
		position: relative;
		flex: 1;
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
		background: #000;
		container-type: inline-size;
	}
	.end-detail {
		max-width: 36rem;
		margin: 0;
		color: var(--muted);
		font-family: var(--font-mono, monospace);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.stats-line {
		position: absolute;
		top: 8px;
		right: 8px;
		z-index: 2;
		margin: 0;
		padding: 3px 9px;
		border-radius: 8px;
		background: rgb(0 0 0 / 0.72);
		color: #fff;
		font-size: var(--text-sm);
		pointer-events: none;
	}
	.screen {
		position: relative;
		flex: 1;
		min-height: 0;
		overflow: hidden;
	}
	canvas {
		position: absolute;
		display: block;
		outline: none;
		touch-action: none;
		user-select: none;
		-webkit-user-select: none;
		-webkit-touch-callout: none;
	}
	canvas.hidden {
		visibility: hidden;
	}
	canvas:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.ime {
		position: absolute;
		left: 0;
		bottom: 0;
		width: 1px;
		height: 1px;
		opacity: 0;
		border: 0;
		padding: 0;
		font-size: 16px;
		pointer-events: none;
	}
	.note {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: 30px;
		text-align: center;
		background: var(--bg);
		color: var(--text);
	}
	.note :global(svg) {
		color: var(--muted);
	}
	.note h3 {
		margin: 0;
	}
	.note p {
		margin: 0;
		max-width: 480px;
		color: var(--muted);
		line-height: 1.5;
	}
	.note progress {
		width: min(320px, 80%);
	}
	.error-text {
		color: var(--error-text) !important;
	}
	.actions {
		display: flex;
		gap: var(--space-2-5);
		flex-wrap: wrap;
		justify-content: center;
	}
	.spinner {
		width: 28px;
		height: 28px;
		border-radius: 50%;
		border: 3px solid color-mix(in srgb, var(--text) 18%, transparent);
		border-top-color: var(--accent);
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.chips {
		position: absolute;
		left: 0;
		right: 0;
		bottom: calc(18px + env(safe-area-inset-bottom));
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2-5);
		pointer-events: none;
	}
	.chip-btn,
	.volume-sheet {
		pointer-events: auto;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface);
		color: var(--text);
		box-shadow: var(--shadow-menu);
	}
	.chip-btn {
		min-height: 36px;
		padding: 0 14px;
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.volume-sheet {
		flex-basis: 100%;
		max-width: 320px;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1-5) var(--space-3);
	}
	.volume-sheet :global(.volume) {
		flex: 1;
	}
	@media (prefers-reduced-motion: reduce) {
		.spinner {
			animation: none;
		}
	}
	.note.start {
		gap: var(--space-2-5);
	}
	.note.start h3 {
		margin: var(--space-1-5) 0 0;
		font-size: var(--text-lg);
	}
	.start-bar {
		position: relative;
		display: block;
		width: 160px;
		height: 4px;
		border-radius: 2px;
		overflow: hidden;
		background: color-mix(in srgb, var(--text) 14%, transparent);
	}
	.start-bar > span {
		position: absolute;
		inset: 0 auto 0 0;
		width: 40%;
		border-radius: 2px;
		background: var(--accent);
		animation: start-bar 1.3s ease-in-out infinite;
	}
	@keyframes start-bar {
		from {
			left: -40%;
		}
		to {
			left: 100%;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.start-bar > span {
			animation: none;
			left: 30%;
		}
	}
	.drop-hint {
		position: absolute;
		inset: 8px;
		z-index: 6;
		display: grid;
		place-items: center;
		border: 2px dashed var(--accent);
		border-radius: var(--radius-tile, 12px);
		background: color-mix(in srgb, var(--bg) 70%, transparent);
		color: var(--text);
		font-size: var(--text-md);
		pointer-events: none;
	}
	.drop-note {
		position: absolute;
		left: 50%;
		bottom: 16px;
		z-index: 6;
		transform: translateX(-50%);
		max-width: calc(100% - 32px);
		margin: 0;
		padding: var(--space-2) 14px;
		border-radius: 10px;
		background: var(--surface);
		border: 1px solid var(--border);
		color: var(--text);
		font-size: var(--text-sm);
	}
</style>
