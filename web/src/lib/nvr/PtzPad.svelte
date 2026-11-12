<script lang="ts">
	// PLAN 79-7: moving a camera that can pan, tilt and zoom. A movement runs
	// while its button is held: the request is repeated every three seconds
	// (the NAS stops a camera it hears nothing of for five) and "stop" follows
	// on release. Presets as a choice.
	import { api, ApiError } from '../api.ts';
	import Icon, { type IconName } from '../components/Icon.svelte';
	import { apiErrorText, t, type MessageKey } from '../i18n/index.svelte.ts';
	import type { PtzInfo } from './types.ts';

	let { camera, name }: { camera: string; name: string } = $props();

	let info = $state<PtzInfo | null>(null);
	let error = $state('');
	const path = $derived(`/nvr/cameras/${encodeURIComponent(camera)}/ptz`);
	$effect(() => {
		const at = path;
		info = null;
		api.get<PtzInfo>(at).then(
			(res) => {
				if (at === path) info = res;
			},
			() => {
				// A NAS without this route, or a camera that does not answer: no pad.
				if (at === path) info = { supported: false, features: [], presets: [] };
			},
		);
	});

	let repeat: ReturnType<typeof setInterval> | undefined;
	let moving = $state('');
	async function send(action: string, preset?: string) {
		try {
			await api.post(path, preset ? { action, preset } : { action });
			error = '';
		} catch (err) {
			// Too many commands at once just drop one; anything else is said.
			if (!(err instanceof ApiError && err.code === 'rate_limited')) error = apiErrorText(err);
		}
	}
	function start(action: string) {
		stop(false);
		moving = action;
		void send(action);
		repeat = setInterval(() => void send(action), 3000);
	}
	function stop(tell = true) {
		clearInterval(repeat);
		repeat = undefined;
		if (moving && tell) void send('stop');
		moving = '';
	}
	$effect(() => () => stop());

	const zoom = $derived(info?.features.includes('zoom') ?? false);
	const MOVES: { action: string; icon: IconName; label: MessageKey; area: string }[] = [
		{ action: 'move_up', icon: 'arrowUp', label: 'nvr.ptz.up', area: 'up' },
		{ action: 'move_left', icon: 'arrowLeft', label: 'nvr.ptz.left', area: 'left' },
		{ action: 'move_right', icon: 'arrowRight', label: 'nvr.ptz.right', area: 'right' },
		{ action: 'move_down', icon: 'arrowDown', label: 'nvr.ptz.down', area: 'down' },
	];
</script>

{#snippet hold(action: string, label: string, icon: IconName | '', text = '', area = '')}
	<button
		type="button"
		class="ptz-btn"
		class:active={moving === action}
		style:grid-area={area || undefined}
		aria-label={label}
		title={label}
		onpointerdown={(e) => {
			e.preventDefault();
			e.currentTarget.setPointerCapture(e.pointerId);
			start(action);
		}}
		onpointerup={() => stop()}
		onpointercancel={() => stop()}
		onkeydown={(e) => {
			if ((e.key === ' ' || e.key === 'Enter') && !e.repeat) start(action);
		}}
		onkeyup={(e) => {
			if (e.key === ' ' || e.key === 'Enter') stop();
		}}
		onblur={() => stop()}
		>{#if icon}<Icon name={icon} size={16} />{:else}{text}{/if}</button
	>
{/snippet}

{#if info?.supported}
	<div class="ptz" role="group" aria-label={t('nvr.ptz.of', { name })}>
		<div class="pad">
			{#each MOVES as move (move.action)}
				{@render hold(move.action, t(move.label), move.icon, '', move.area)}
			{/each}
		</div>
		{#if zoom}
			<div class="zoom">
				{@render hold('zoom_in', t('nvr.ptz.zoomIn'), '', '+')}
				{@render hold('zoom_out', t('nvr.ptz.zoomOut'), '', '−')}
			</div>
		{/if}
		{#if info.presets.length}
			<select
				class="field presets"
				aria-label={t('nvr.ptz.presets')}
				onchange={(e) => {
					const preset = e.currentTarget.value;
					e.currentTarget.value = '';
					if (preset) void send('preset', preset);
				}}
			>
				<option value="">{t('nvr.ptz.presets')}</option>
				{#each info.presets as preset (preset)}<option value={preset}>{preset}</option>{/each}
			</select>
		{/if}
		{#if error}<p class="ptz-error" role="alert">{error}</p>{/if}
	</div>
{/if}

<style>
	.ptz {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2);
		border-radius: 10px;
		background: rgb(11 15 20 / 0.78);
		color: #fff;
	}
	.pad {
		display: grid;
		grid-template: 'a up b' 30px 'left c right' 30px 'd down e' 30px / 30px 30px 30px;
		gap: 2px;
	}
	.zoom {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.ptz-btn {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		padding: 0;
		border: 1px solid rgb(255 255 255 / 0.25);
		border-radius: 7px;
		background: rgb(255 255 255 / 0.1);
		color: #fff;
		font: inherit;
		font-weight: 600;
		cursor: pointer;
		touch-action: none;
	}
	.ptz-btn.active,
	.ptz-btn:hover {
		background: var(--accent);
		border-color: var(--accent);
	}
	.presets {
		max-width: 11rem;
		min-height: 30px;
	}
	.ptz-error {
		flex-basis: 100%;
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
