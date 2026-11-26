<script lang="ts">
	import { t } from '../i18n/index.svelte.ts';
	import LiveVideo from './LiveVideo.svelte';
	import type { Camera } from './types.ts';
	import type { NvrPrefs } from './layout.ts';
	import { cameraCondition, labelName } from './texts.ts';

	let {
		camera,
		running,
		prefs,
		objects = [],
		arranging,
		onopen,
	}: {
		camera: Camera;
		running: boolean;
		prefs: NvrPrefs;
		objects?: string[];
		arranging: boolean;
		onopen: () => void;
	} = $props();
	let mode = $state<'live' | 'still' | 'none'>('none');
	const condition = $derived(cameraCondition(camera, running));
	const recording = $derived(
		condition === 'online' && !!camera.record && camera.record.mode !== 'off',
	);
</script>

{#snippet preview()}
	<div class="picture">
		{#if condition !== 'off'}
			<LiveVideo
				id={camera.id}
				name={camera.name}
				height={240}
				sub
				quiet={condition === 'offline'}
				onmode={(v) => (mode = v)}
			/>
		{/if}
		{#if prefs.recDot && recording}<span class="recording" title={t('nvr.live.recording')}
				><span class="sr-only">{t('nvr.live.recording')}</span></span
			>{/if}
	</div>
	<div class="caption" id={`nvr-caption-${camera.id}`}>
		{#if prefs.names}<strong>{camera.name}</strong>{/if}
		<small
			>{t(`nvr.camera.state.${condition}`)}{#if mode === 'still' && condition === 'online'}
				· {t('nvr.live.still')}{/if}</small
		>
		{#if prefs.marks && running && camera.enabled && objects.length}<small class="mark"
				>{objects.map(labelName).join(' · ')}</small
			>{/if}
	</div>
{/snippet}

{#if arranging}
	<div class="preview" data-camera={camera.id}>{@render preview()}</div>
{:else}
	<button
		type="button"
		class="preview"
		data-camera={camera.id}
		aria-label={t('nvr.live.solo', { name: camera.name })}
		aria-describedby={`nvr-caption-${camera.id}`}
		onclick={onopen}>{@render preview()}</button
	>
{/if}

<style>
	.preview {
		min-width: 0;
		position: relative;
		height: 100%;
		border: 1px solid var(--border);
		border-radius: 15px;
		overflow: hidden;
		background: color-mix(in srgb, var(--surface) 85%, transparent);
	}
	.picture {
		position: relative;
		aspect-ratio: 16/9;
		background: #0b0f14;
	}
	.caption {
		display: grid;
		align-content: start;
		gap: 2px;
		padding: 8px 10px;
	}
	.caption > * {
		overflow-wrap: anywhere;
	}
	small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.recording {
		position: absolute;
		top: 8px;
		right: 8px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: #ee574e;
		box-shadow: 0 0 0 2px #121b24;
	}
	button.preview {
		display: grid;
		align-content: start;
		width: 100%;
		padding: 0;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.preview:focus-visible {
		outline: 3px solid var(--accent);
		outline-offset: -3px;
	}
</style>
