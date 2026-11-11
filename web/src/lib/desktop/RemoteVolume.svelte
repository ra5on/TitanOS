<script lang="ts">
	// The remote view's sound: mute and the volume slider with its value.
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import type { RemoteSession } from './remote-session.svelte.ts';

	let { s }: { s: RemoteSession } = $props();
</script>

<button
	type="button"
	class="icon-btn"
	onclick={s.toggleMute}
	aria-pressed={s.prefs.muted}
	title={s.prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
	aria-label={s.prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
>
	<Icon name="volume" size={16} />
	{#if s.prefs.muted}<span class="slash" aria-hidden="true"></span>{/if}
</button>
<input
	class="volume"
	type="range"
	min="0"
	max="100"
	step="1"
	value={Math.round((s.prefs.muted ? 0 : s.prefs.volume) * 100)}
	aria-label={t('viewer.volume')}
	aria-valuetext={s.volumeText}
	oninput={(e) => {
		s.startAudio();
		s.setVolume(Number(e.currentTarget.value) / 100);
	}}
/>
<span class="pct mono">{s.volumeText}</span>

<style>
	.volume {
		width: 110px;
	}
	.pct {
		width: 3.2em;
		text-align: right;
		color: var(--muted);
	}
	.icon-btn {
		position: relative;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border: 0;
		border-radius: 7px;
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.icon-btn:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.icon-btn:disabled {
		opacity: 0.5;
	}
	.slash {
		position: absolute;
		width: 2px;
		height: 60%;
		background: currentColor;
		transform: rotate(45deg);
	}
</style>
