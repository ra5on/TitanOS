<script lang="ts">
	// The settings of the KI-Agent inside the agent (PLAN 53): speech, models,
	// what it plans and knows, and gestures, motion and text size of the app
	// (since PLAN 68 no colors or glass of its own: the TitanOS theme). Everything
	// here belongs to the account and is kept on the NAS (GET/PUT /agent/prefs,
	// memory, skills …); vibration, screen and text size belong to the device.
	// The connection to Ollama and the rights stay under TitanOS › Einstellungen.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { SWIPES, type Look, type Status, type Swipe } from '#lib/agent.ts';
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import InitiativeSettings from './InitiativeSettings.svelte';
	import PushDevices from '#lib/components/PushDevices.svelte';
	import FeaturesPanel from './features/FeaturesPanel.svelte';

	type Part = 'memory' | 'skills' | 'schedules' | 'map' | 'me' | 'log';
	let {
		look = $bindable(),
		status,
		user,
		calm = false,
		start = 'list',
		onopen,
		onclose,
		onknow,
		chats = 0,
		ondeleteall,
	}: {
		/** Own chats of the account, for "Alle Chats löschen" (PLAN 56). */
		chats?: number;
		ondeleteall?: () => void;
		look: Look;
		status: Status;
		user: string;
		calm?: boolean;
		start?: 'list' | 'speech' | 'look' | 'models' | 'initiative' | 'mind' | 'push' | 'features';
		onopen: (part: Part) => void;
		onclose: () => void;
		/** Starts getting to know the person again (PLAN 53). */
		onknow?: () => void;
	} = $props();

	let section = $state<
		'list' | 'speech' | 'look' | 'models' | 'initiative' | 'mind' | 'push' | 'features'
	>((() => start)());
	const rows: {
		id: Part | 'speech' | 'look' | 'models' | 'initiative' | 'mind' | 'push' | 'features';
		icon: IconName;
		label: MessageKey;
		sub?: string;
	}[] = $derived([
		{
			id: 'speech',
			icon: 'volume',
			label: 'agent.set.speech',
			sub: look.read ? t('agent.set.readAuto') : t('agent.set.readButton'),
		},
		{
			id: 'models',
			icon: 'chip',
			label: 'agent.set.models',
			sub: status.vision ? `${status.model} · ${status.vision}` : status.model,
		},
		// Mitteilungen auf diesem Gerät: on an iPhone the agent app from the
		// home screen is an app of its own and needs its own subscription.
		{
			id: 'push' as const,
			icon: 'bell' as IconName,
			label: 'agent.set.push' as MessageKey,
			sub: t('agent.set.pushSub'),
		},
		// PLAN 54: the nine functions, each off by default.
		{
			id: 'features',
			icon: 'sparkle',
			label: 'agent.f.title',
			sub: t('agent.f.sub'),
		},
		{ id: 'initiative', icon: 'bulb', label: 'agent.set.initiative' },
		{ id: 'schedules', icon: 'clock', label: 'agent.schedules' },
		{ id: 'mind', icon: 'users', label: 'agent.set.mind' },
		{ id: 'memory', icon: 'star', label: 'agent.memory' },
		{ id: 'skills', icon: 'zap', label: 'agent.skills' },
		{ id: 'me', icon: 'chat', label: 'agent.me' },
		{ id: 'map', icon: 'globe', label: 'agent.map' },
		...(status.admin
			? [
					{
						id: 'log' as const,
						icon: 'list' as IconName,
						label: 'agent.log' as MessageKey,
					},
				]
			: []),
		{
			id: 'look',
			icon: 'palette',
			label: 'agent.set.look',
			sub: `${look.gestures ? t('agent.set.gesturesOn') : t('agent.set.gesturesOff')} · ${t(`agent.set.motion.${look.motion}`)}`,
		},
	]);
	function go(id: (typeof rows)[number]['id']) {
		if (
			id === 'speech' ||
			id === 'look' ||
			id === 'models' ||
			id === 'initiative' ||
			id === 'mind' ||
			id === 'push' ||
			id === 'features'
		)
			((back = false), (section = id));
		else onopen(id);
	}
	// Which way the next sub-item slides in.
	let back = $state(false);
	function goBack() {
		back = true;
		section = 'list';
	}
	const swipeLabel = (s: Swipe) => t(`agent.set.swipe.${s}`);
	const title = $derived(
		section === 'speech'
			? t('agent.set.speech')
			: section === 'look'
				? t('agent.set.look')
				: section === 'initiative'
					? t('agent.set.initiative')
					: section === 'mind'
						? t('agent.set.mind')
						: section === 'models'
							? t('agent.set.models')
							: section === 'push'
								? t('agent.set.push')
								: section === 'features'
									? t('agent.f.title')
									: t('agent.set.title'),
	);
</script>

<svelte:window
	onkeydown={(e) =>
		e.key === 'Escape' && !document.querySelector('dialog[open], .over-settings') && onclose()}
/>

<div class="scrim" role="presentation" onclick={onclose} transition:fly={{ duration: 0 }}></div>
<div
	class="panel"
	role="dialog"
	aria-modal="true"
	aria-label={title}
	transition:fly={{ x: 380, duration: calm ? 0 : 260, opacity: 1 }}
>
	<header>
		<button
			type="button"
			class="ib"
			aria-label={section === 'list' ? t('common.close') : t('agent.set.back')}
			onclick={() => (section === 'list' ? onclose() : goBack())}
			><Icon name={section === 'list' ? 'close' : 'chevronLeft'} size={20} /></button
		>
		<h2>{title}</h2>
		<span class="ib-space"></span>
	</header>

	<div class="body">
		<!-- A sub-item slides in from the right, "back" from the left (PLAN 56). -->
		{#key section}
			<div
				class="pane"
				in:fly={{ x: back ? -90 : 140, duration: calm ? 0 : 300, opacity: 0, easing: cubicOut }}
			>
				{#if section === 'list'}
					<div class="who">
						<span class="av" aria-hidden="true">{user.slice(0, 1).toUpperCase()}</span>
						<span><strong>{user}</strong><small>{status.model}</small></span>
					</div>
					<div class="grp">
						{#each rows as r (r.id)}
							<button type="button" class="row" onclick={() => go(r.id)}>
								<span class="sq" aria-hidden="true"><Icon name={r.icon} size={17} /></span>
								<span class="t"
									><strong>{t(r.label)}</strong>{#if r.sub}<small>{r.sub}</small>{/if}</span
								>
								<Icon name="chevronRight" size={16} />
							</button>
						{/each}
					</div>
					{#if ondeleteall && chats > 0}
						<div class="grp">
							<button type="button" class="row danger" onclick={ondeleteall}>
								<span class="sq" aria-hidden="true"><Icon name="trash" size={17} /></span>
								<span class="t"
									><strong>{t('agent.deleteAll')}</strong><small
										>{t('agent.deleteAllSub', { count: chats })}</small
									></span
								>
							</button>
						</div>
					{/if}
					{#if status.admin}
						<a class="titanos" href="/settings/agent"
							><Icon name="gear" size={16} />{t('agent.set.titanos')}</a
						>
					{/if}
				{:else if section === 'speech'}
					<div class="grp">
						<label class="row">
							<span class="t"
								><strong>{t('agent.set.readAutoLabel')}</strong><small
									>{t('agent.set.readAutoHint')}</small
								></span
							>
							<input type="checkbox" class="switch" bind:checked={look.read} />
						</label>
						<label class="row col">
							<span class="t"
								><strong>{t('agent.set.rate')}</strong><small>{look.rate.toFixed(1)}×</small></span
							>
							<input type="range" min="0.5" max="2" step="0.1" bind:value={look.rate} />
						</label>
					</div>
					<p class="cap">{t('agent.set.voiceIn')}</p>
					<div class="grp">
						<div class="row">
							<span class="t"
								><strong>{t('agent.set.whisper')}</strong><small
									>{status.voice ? t('agent.set.whisperOn') : t('agent.set.whisperOff')}</small
								></span
							>
							{#if !status.voice && status.admin}<a
									class="btn btn-primary small"
									href="/settings/agent?tab=connection#voice">{t('agent.set.setup')}</a
								>{/if}
						</div>
						<div class="row col">
							<span class="t"><strong>{t('agent.set.mic')}</strong></span>
							<span class="seg" role="radiogroup" aria-label={t('agent.set.mic')}>
								{#each ['tap', 'hold'] as const as m (m)}
									<label class:on={look.mic === m}
										><input type="radio" bind:group={look.mic} value={m} />{t(
											`agent.set.mic.${m}`,
										)}</label
									>
								{/each}
							</span>
						</div>
					</div>
				{:else if section === 'initiative' || section === 'mind'}
					<InitiativeSettings part={section} onrestart={onknow} />
				{:else if section === 'features'}
					<FeaturesPanel admin={status.admin} {calm} />
				{:else if section === 'push'}
					<p class="pushhint">{t('agent.set.pushHint')}</p>
					<PushDevices personal={!status.admin} />
				{:else if section === 'models'}
					<div class="grp">
						<div class="row">
							<span class="t"
								><strong>{t('agent.set.chatModel')}</strong><small>{status.model}</small></span
							>
						</div>
						<div class="row">
							<span class="t"
								><strong>{t('agent.set.visionModel')}</strong><small
									>{status.vision || t('agent.set.visionSame')}</small
								></span
							>
						</div>
					</div>
					<p class="hint">{t('agent.set.modelsHint')}</p>
					{#if status.admin}<a class="titanos" href="/settings/agent?tab=connection"
							><Icon name="gear" size={16} />{t('agent.set.modelsAdmin')}</a
						>{/if}
				{:else}
					<p class="cap">{t('agent.set.gestures')}</p>
					<div class="grp">
						<label class="row">
							<span class="t"
								><strong>{t('agent.set.gesturesLabel')}</strong><small
									>{t('agent.set.gesturesHint')}</small
								></span
							>
							<input type="checkbox" class="switch" bind:checked={look.gestures} />
						</label>
						{#each [['swipeRight', 'agent.set.swipeRight'], ['swipeLeft', 'agent.set.swipeLeft']] as const as [key, label] (key)}
							<label class="row" class:off={!look.gestures}>
								<span class="t"><strong>{t(label)}</strong></span>
								<select class="field" bind:value={look[key]} disabled={!look.gestures}>
									{#each SWIPES as s (s)}<option value={s}>{swipeLabel(s)}</option>{/each}
								</select>
							</label>
						{/each}
						<label class="row" class:off={!look.gestures}>
							<span class="t"
								><strong>{t('agent.set.edge')}</strong><small>{t('agent.set.edgeHint')}</small
								></span
							>
							<input
								type="checkbox"
								class="switch"
								bind:checked={look.edge}
								disabled={!look.gestures}
							/>
						</label>
						<label class="row">
							<span class="t"><strong>{t('agent.set.pull')}</strong></span>
							<input type="checkbox" class="switch" bind:checked={look.pull} />
						</label>
						<label class="row">
							<span class="t"
								><strong>{t('agent.lookVibrate')}</strong><small>{t('agent.set.device')}</small
								></span
							>
							<input type="checkbox" class="switch" bind:checked={look.vibrate} />
						</label>
					</div>

					<p class="cap">{t('agent.set.lookMotion')}</p>
					<div class="grp">
						<div class="row col">
							<span class="t"><strong>{t('agent.set.motionLabel')}</strong></span>
							<span class="seg" role="radiogroup" aria-label={t('agent.set.motionLabel')}>
								{#each ['full', 'reduced', 'off'] as const as m (m)}
									<label class:on={look.motion === m}
										><input type="radio" bind:group={look.motion} value={m} />{t(
											`agent.set.motion.${m}`,
										)}</label
									>
								{/each}
							</span>
						</div>
						<div class="row col">
							<span class="t"
								><strong>{t('agent.set.fontSize')}</strong><small>{t('agent.set.device')}</small
								></span
							>
							<span class="seg" role="radiogroup" aria-label={t('agent.set.fontSize')}>
								{#each ['s', 'm', 'l'] as const as f (f)}
									<label class:on={look.fontSize === f}
										><input type="radio" bind:group={look.fontSize} value={f} />{t(
											`agent.set.font.${f}`,
										)}</label
									>
								{/each}
							</span>
						</div>
						<label class="row">
							<span class="t"><strong>{t('agent.set.compact')}</strong></span>
							<input type="checkbox" class="switch" bind:checked={look.compact} />
						</label>
					</div>

					<p class="cap">{t('agent.set.startChat')}</p>
					<div class="grp">
						<div class="row col">
							<span class="t"><strong>{t('agent.set.startLabel')}</strong></span>
							<span class="seg" role="radiogroup" aria-label={t('agent.set.startLabel')}>
								{#each ['new', 'last', 'overview'] as const as v (v)}
									<label class:on={look.start === v}
										><input type="radio" bind:group={look.start} value={v} />{t(
											`agent.set.start.${v}`,
										)}</label
									>
								{/each}
							</span>
						</div>
						<label class="row">
							<span class="t"><strong>{t('agent.set.suggestions')}</strong></span>
							<input type="checkbox" class="switch" bind:checked={look.suggestions} />
						</label>
						<label class="row">
							<span class="t"><strong>{t('agent.set.steps')}</strong></span>
							<input type="checkbox" class="switch" bind:checked={look.steps} />
						</label>
						<label class="row">
							<span class="t"><strong>{t('agent.lookTyping')}</strong></span>
							<input type="checkbox" class="switch" bind:checked={look.typing} />
						</label>
						<label class="row">
							<span class="t"
								><strong>{t('agent.set.keepAwake')}</strong><small>{t('agent.set.device')}</small
								></span
							>
							<input type="checkbox" class="switch" bind:checked={look.keepAwake} />
						</label>
					</div>
				{/if}
			</div>
		{/key}
	</div>
</div>

<style>
	/* PLAN 68: a sheet from the right with cards and rows like every
	   settings page; colors from the theme only. */
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.45);
	}
	.panel {
		position: fixed;
		z-index: 71;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(440px, 100%);
		display: flex;
		flex-direction: column;
		color: var(--text);
		background: var(--bg);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu, -18px 0 44px rgb(0 0 0 / 0.5));
	}
	header {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: calc(env(safe-area-inset-top, 0px) + 10px) 14px 10px;
		border-bottom: 1px solid var(--border);
	}
	h2 {
		flex: 1;
		margin: 0;
		text-align: center;
		font-size: var(--text-md);
	}
	.ib,
	.ib-space {
		width: 36px;
		height: 36px;
		flex: none;
	}
	.ib {
		border-radius: var(--radius-ctl);
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.ib:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.body {
		flex: 1;
		overflow-y: auto;
		padding: 16px 16px calc(env(safe-area-inset-bottom, 0px) + 18px);
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.body > *,
	.pane > * {
		flex-shrink: 0;
	}
	.pane {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 16px;
		margin-bottom: 8px;
		border-radius: var(--radius-tile);
		background: var(--surface);
		border: 1px solid var(--border);
	}
	.who span:last-child {
		display: flex;
		flex-direction: column;
	}
	.who small,
	.t small,
	.hint {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.av {
		width: 40px;
		height: 40px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-weight: 700;
		color: var(--on-accent, #fff);
		background: var(--accent);
	}
	.cap {
		margin: 12px 4px 2px;
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.05em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.pushhint {
		margin: 0 4px 12px;
		font-size: var(--text-base);
		line-height: 1.45;
		color: var(--muted);
	}
	.grp {
		border-radius: var(--radius-tile);
		overflow: hidden;
		background: var(--surface);
		border: 1px solid var(--border);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 48px;
		padding: 10px 16px;
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	button.row:hover {
		background: color-mix(in srgb, var(--text) 5%, transparent);
	}
	.row:first-child {
		border-top: 0;
	}
	.row.off {
		opacity: 0.55;
	}
	/* Choices with long labels go below the text, never beside it. */
	.row.col {
		flex-wrap: wrap;
	}
	.row.col > .seg,
	.row.col > input[type='range'] {
		flex-basis: 100%;
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.t strong {
		font-weight: 500;
	}
	.sq {
		width: 30px;
		height: 30px;
		border-radius: var(--radius-ctl);
		flex: none;
		display: grid;
		place-items: center;
		color: var(--muted);
		background: var(--chip);
	}
	.row > :global(svg) {
		color: var(--muted);
	}
	/* A compact segmented control like the rest of TitanOS. */
	.seg {
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: 1fr;
		gap: 2px;
		padding: 3px;
		border-radius: var(--radius-ctl);
		background: var(--chip);
	}
	.seg label {
		text-align: center;
		white-space: nowrap;
		padding: 6px 8px;
		border-radius: calc(var(--radius-ctl) - 2px);
		font-size: var(--text-sm);
		color: var(--text);
		cursor: pointer;
	}
	.seg label.on {
		background: var(--surface);
		font-weight: 600;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.3);
	}
	.seg input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.seg label:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	input[type='range'] {
		accent-color: var(--accent);
	}
	.titanos {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin-top: 12px;
		min-height: 40px;
		border-radius: var(--radius-ctl);
		border: 1px solid var(--border);
		color: var(--text);
		text-decoration: none;
	}
	.titanos:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.small {
		min-height: 32px;
		padding: 0 12px;
	}
	.field {
		max-width: 46%;
	}
	.row.danger strong {
		color: var(--error-text);
	}
</style>
