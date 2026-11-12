<script lang="ts">
	// PLAN 79-7 (drawing 79-nvr/live2): one camera in the live view – the
	// picture, its name in the picture, what it sees right now, the red dot
	// while it records, and its buttons: sound, talk, move, full size.
	import Icon from '../components/Icon.svelte';
	import { i18n, t, type MessageKey } from '../i18n/index.svelte.ts';
	import type { Activity } from './feed.svelte.ts';
	import type { NvrPrefs } from './layout.ts';
	import LiveVideo from './LiveVideo.svelte';
	import PtzPad from './PtzPad.svelte';
	import { openTalk, talkBlocked, type Talk, type TalkProblem } from './talk.ts';
	import { cameraCondition, labelName, sinceText } from './texts.ts';
	import type { Camera } from './types.ts';

	let {
		camera,
		running,
		prefs,
		activity,
		/** While the tiles are arranged the picture has no buttons. */
		arranging = false,
		/** This tile fills the view. */
		solo = false,
		/** The small stream is enough. */
		small = false,
		onsolo,
	}: {
		camera: Camera;
		running: boolean;
		prefs: NvrPrefs;
		activity?: Activity;
		arranging?: boolean;
		solo?: boolean;
		small?: boolean;
		onsolo?: () => void;
	} = $props();

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const condition = $derived(cameraCondition(camera, running));
	let shown = $state<'live' | 'still' | 'none'>('none');
	let sound = $state(false);
	let ptzOpen = $state(false);
	const recording = $derived(
		running &&
			camera.enabled &&
			camera.state.online &&
			!!camera.record &&
			camera.record.mode !== 'off',
	);
	const seen = $derived(running && camera.enabled ? (activity?.objects ?? []) : []);

	// ---- Talk (push to talk) ----
	let talk: Talk | null = null;
	let talking = $state(false);
	let talkState = $state<'' | 'opening' | 'ready'>('');
	let talkProblem = $state<TalkProblem | ''>('');
	let idle: ReturnType<typeof setTimeout> | undefined;
	let wants = false;
	function endTalk() {
		clearTimeout(idle);
		talk?.close();
		talk = null;
		talking = false;
		talkState = '';
	}
	async function press() {
		wants = true;
		talkProblem = talkBlocked();
		if (talkProblem) return;
		clearTimeout(idle);
		if (!talk) {
			talkState = 'opening';
			try {
				talk = await openTalk(camera.id, (problem) => {
					talk = null;
					talking = false;
					talkState = '';
					talkProblem = problem;
				});
				talkState = 'ready';
			} catch (problem) {
				talkState = '';
				talkProblem = (typeof problem === 'string' ? problem : 'failed') as TalkProblem;
				return;
			}
		}
		// Released while the connection was still opening: stay quiet.
		if (wants) {
			talk.speak(true);
			talking = true;
		} else release();
	}
	function release() {
		wants = false;
		if (!talk) return;
		talk.speak(false);
		talking = false;
		// The connection stays for the next sentence, then closes.
		clearTimeout(idle);
		idle = setTimeout(endTalk, 30000);
	}
	$effect(() => () => endTalk());
</script>

<div class="tile-view" class:solo data-camera={camera.id}>
	{#if condition !== 'off'}
		<LiveVideo
			id={camera.id}
			name={camera.name}
			sub={small}
			{sound}
			fit={solo ? 'contain' : 'cover'}
			height={solo ? 1080 : 480}
			onmode={(mode) => (shown = mode)}
		/>
	{/if}
	{#if condition === 'off'}
		<p class="state">{t('nvr.camera.stateOff')}</p>
	{:else if condition === 'offline'}
		<p class="state" class:over={shown !== 'none'}>
			{camera.state.offlineSince
				? t('nvr.camera.offlineSince', { time: sinceText(camera.state.offlineSince, locale) })
				: t('nvr.camera.stateOffline')}
		</p>
	{/if}
	{#if prefs.names}<span class="name">{camera.name}</span>{/if}
	{#if prefs.recDot && recording}<span class="rec" title={t('nvr.live.recording')}
			><span class="sr-only">{t('nvr.live.recording')}</span></span
		>{/if}
	{#if prefs.marks && seen.length}
		<span class="marks"
			>{#each seen as label (label)}<span class="mark">{labelName(label)}</span>{/each}</span
		>
	{/if}
	{#if shown === 'still' && condition === 'online' && !arranging}
		<span class="mode" title={t('nvr.live.stillHint')}>{t('nvr.live.still')}</span>
	{/if}
	{#if !arranging && condition !== 'off'}
		<div class="tools">
			{#if ptzOpen && camera.ptz}<div class="ptz-pop">
					<PtzPad camera={camera.id} name={camera.name} />
				</div>{/if}
			{#if talkProblem}
				<p class="talk-note" role="alert">
					{t(`nvr.talk.${talkProblem}` as MessageKey)}
					<button
						type="button"
						class="plain"
						aria-label={t('common.close')}
						onclick={() => (talkProblem = '')}><Icon name="close" size={13} /></button
					>
				</p>
			{/if}
			<div class="buttons">
				{#if camera.audio.enabled}
					<button
						type="button"
						class="tool"
						class:on={sound}
						aria-pressed={sound}
						aria-label={t('nvr.live.sound', { name: camera.name })}
						title={t(sound ? 'nvr.live.soundOff' : 'nvr.live.soundOn')}
						onclick={() => (sound = !sound)}><Icon name="volume" size={16} /></button
					>
				{/if}
				{#if camera.audio.twoWay}
					<button
						type="button"
						class="tool"
						class:on={talking}
						class:busy={talkState === 'opening'}
						aria-pressed={talking}
						aria-label={t('nvr.talk.hold', { name: camera.name })}
						title={t('nvr.talk.holdHint')}
						onpointerdown={(e) => {
							e.preventDefault();
							e.currentTarget.setPointerCapture(e.pointerId);
							void press();
						}}
						onpointerup={release}
						onpointercancel={release}
						onkeydown={(e) => {
							if ((e.key === ' ' || e.key === 'Enter') && !e.repeat) void press();
						}}
						onkeyup={(e) => {
							if (e.key === ' ' || e.key === 'Enter') release();
						}}
						onblur={release}><Icon name="mic" size={16} /></button
					>
				{/if}
				{#if camera.ptz}
					<button
						type="button"
						class="tool"
						class:on={ptzOpen}
						aria-pressed={ptzOpen}
						aria-label={t('nvr.ptz.of', { name: camera.name })}
						title={t('nvr.ptz.title')}
						onclick={() => (ptzOpen = !ptzOpen)}><Icon name="resize" size={16} /></button
					>
				{/if}
				{#if onsolo}
					<button
						type="button"
						class="tool"
						aria-pressed={solo}
						aria-label={t(solo ? 'nvr.live.soloOff' : 'nvr.live.solo', { name: camera.name })}
						title={t(solo ? 'nvr.live.soloOffShort' : 'nvr.live.soloShort')}
						onclick={onsolo}><Icon name={solo ? 'close' : 'maximize'} size={16} /></button
					>
				{/if}
			</div>
		</div>
	{/if}
</div>

<style>
	.tile-view {
		position: absolute;
		inset: 0;
		border-radius: 10px;
		overflow: hidden;
		background: #0b0f14;
		color: #fff;
	}
	.tile-view.solo {
		border-radius: 0;
	}
	.state {
		position: absolute;
		inset: 0;
		margin: 0;
		display: grid;
		place-items: center;
		padding: var(--space-3) var(--space-3) 44px;
		text-align: center;
		font-size: var(--text-sm);
		color: #c9d1d9;
		overflow-wrap: anywhere;
	}
	.state.over {
		inset: auto 0 44px 0;
		padding: var(--space-1) var(--space-3);
		background: rgb(11 15 20 / 0.7);
	}
	.name,
	.mode,
	.mark {
		padding: 2px 8px;
		border-radius: 6px;
		background: rgb(11 15 20 / 0.72);
		color: #fff;
		font-size: var(--text-sm);
		line-height: 1.5;
		overflow-wrap: anywhere;
	}
	.name {
		position: absolute;
		left: 8px;
		bottom: 8px;
		max-width: calc(100% - 16px);
	}
	.tile-view:has(.tools) .name {
		bottom: 46px;
	}
	.rec {
		position: absolute;
		top: 10px;
		right: 10px;
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: #f04438;
		box-shadow: 0 0 0 2px rgb(11 15 20 / 0.5);
	}
	.marks {
		position: absolute;
		top: 8px;
		left: 8px;
		right: 30px;
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.mark {
		background: #1f9d6b;
		font-weight: 600;
	}
	.mode {
		position: absolute;
		top: 8px;
		right: 28px;
		font-size: var(--text-xs, 0.72rem);
		color: #c9d1d9;
	}
	.marks ~ .mode {
		top: auto;
		bottom: 46px;
		right: 8px;
	}
	.tools {
		position: absolute;
		right: 6px;
		bottom: 6px;
		left: 6px;
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 6px;
		pointer-events: none;
	}
	.tools > * {
		pointer-events: auto;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 4px;
	}
	.tool {
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		padding: 0;
		border: 0;
		border-radius: 8px;
		background: rgb(11 15 20 / 0.72);
		color: #fff;
		cursor: pointer;
		touch-action: none;
	}
	.tool:hover,
	.tool.on {
		background: var(--accent);
	}
	.tool.busy {
		opacity: 0.6;
	}
	.tool:focus-visible {
		outline: 2px solid #fff;
		outline-offset: 1px;
	}
	.talk-note {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		max-width: 100%;
		margin: 0;
		padding: 6px 10px;
		border-radius: 8px;
		background: rgb(11 15 20 / 0.86);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.plain {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		padding: 0;
		border: 0;
		border-radius: 6px;
		background: none;
		color: inherit;
		cursor: pointer;
	}
	.ptz-pop {
		max-width: 100%;
	}
</style>
