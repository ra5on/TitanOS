<script lang="ts">
	// The settings of the KI-Agent as a page (PLAN 90-4), grouped like the
	// control panel: model and speech, behaviour, this device, the NAS (a
	// link for owners and administrators) and the data. Each row opens its
	// part as a page of its own (/agent?page=settings&part=…). What stood
	// here twice before is on its own page now: Geplant, Gedächtnis (with
	// its settings) and Wissen (Skills, NAS-Landkarte, Protokoll, Meine
	// Anweisung). Everything belongs to the account and is kept on the NAS;
	// vibration, screen and text size belong to the device.
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import PushDevices from '#lib/components/PushDevices.svelte';
	import InitiativeSettings from '../InitiativeSettings.svelte';
	import FeaturesPanel from '../features/FeaturesPanel.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { levelLabel, SWIPES, type Look, type Status, type Swipe } from '#lib/agent.ts';
	import PageShell from './PageShell.svelte';

	const PARTS = [
		'models',
		'speech',
		'personality',
		'initiative',
		'features',
		'push',
		'look',
		'gestures',
	] as const;
	type Part = (typeof PARTS)[number];

	let {
		look = $bindable(),
		status,
		part: partName = '',
		calm = false,
		phone = false,
		chats = 0,
		onunfold,
		onknow,
		ondeleteall,
	}: {
		look: Look;
		status: Status;
		/** The part that is open; '' is the list. */
		part?: string;
		calm?: boolean;
		phone?: boolean;
		/** Own chats of the account, for "Alle Chats löschen" (PLAN 56). */
		chats?: number;
		onunfold?: () => void;
		/** Starts getting to know the person again (PLAN 53). */
		onknow?: () => void;
		ondeleteall?: () => void;
	} = $props();

	const part = $derived(PARTS.includes(partName as Part) ? (partName as Part) : '');
	const href = (p: Part) => `/agent?page=settings&part=${p}`;
	const titles: Record<Part, string> = $derived({
		models: t('agent.set.models'),
		speech: t('agent.set.speech'),
		personality: t('agent.init.personality'),
		initiative: t('agent.set.initiative'),
		features: t('agent.f.title'),
		push: t('agent.set.push'),
		look: t('agent.set.lookTitle'),
		gestures: t('agent.set.gestures'),
	});
	const swipeLabel = (s: Swipe) => t(`agent.set.swipe.${s}`);
</script>

{#if !part}
	<PageShell title={t('agent.set.title')} lead={t('agent.set.lead')} {phone} {onunfold}>
		<SettingGroup title={t('agent.set.groupModel')}>
			<SettingRow
				icon="chip"
				title={t('agent.set.models')}
				desc={t('agent.set.modelsDesc')}
				summary={phone
					? ''
					: status.think
						? `${status.model} · ${levelLabel(status.think, i18n.locale)}`
						: status.model}
				href={href('models')}
			/>
			<SettingRow
				icon="volume"
				title={t('agent.set.speech')}
				desc={t('agent.set.speechDesc')}
				summary={phone ? '' : look.read ? t('agent.set.readAuto') : t('agent.set.readButton')}
				href={href('speech')}
			/>
		</SettingGroup>
		<SettingGroup title={t('agent.set.groupBehaviour')}>
			<SettingRow
				icon="star"
				title={t('agent.init.personality')}
				desc={t('agent.set.personalityDesc')}
				href={href('personality')}
			/>
			<SettingRow
				icon="activity"
				title={t('agent.set.initiative')}
				desc={t('agent.set.initiativeDesc')}
				href={href('initiative')}
			/>
			<SettingRow
				icon="grid"
				title={t('agent.f.title')}
				desc={t('agent.f.sub')}
				href={href('features')}
			/>
		</SettingGroup>
		<SettingGroup title={t('agent.set.groupDevice')}>
			<SettingRow
				icon="bell"
				title={t('agent.set.push')}
				desc={t('agent.set.pushSub')}
				href={href('push')}
			/>
			<SettingRow
				icon="eye"
				title={t('agent.set.lookTitle')}
				desc={t('agent.set.lookDesc')}
				summary={phone
					? ''
					: `${t(`agent.set.font.${look.fontSize}`)} · ${t(`agent.set.motion.${look.motion}`)}`}
				href={href('look')}
			/>
			<SettingRow
				icon="move"
				title={t('agent.set.gestures')}
				desc={t('agent.set.gesturesDesc')}
				summary={phone
					? ''
					: look.gestures
						? t('agent.set.gesturesOn')
						: t('agent.set.gesturesOff')}
				href={href('gestures')}
			/>
			<SettingRow icon="home" title={t('agent.set.startLabel')} desc={t('agent.set.startDesc')}>
				{#snippet control()}<span
						class="seg"
						role="radiogroup"
						aria-label={t('agent.set.startLabel')}
					>
						{#each ['new', 'last', 'overview'] as const as v (v)}
							<label class:on={look.start === v}
								><input type="radio" bind:group={look.start} value={v} />{t(
									`agent.set.start.${v}`,
								)}</label
							>
						{/each}
					</span>{/snippet}
			</SettingRow>
		</SettingGroup>
		{#if status.admin}
			<SettingGroup title={t('agent.set.groupNas')}>
				<SettingRow
					icon="external"
					title={t('agent.set.nas')}
					desc={t('agent.set.nasDesc')}
					href="/settings/agent"
				/>
			</SettingGroup>
		{/if}
		{#if ondeleteall}
			<SettingGroup title={t('agent.set.groupData')}>
				<SettingRow
					icon="trash"
					title={t('agent.deleteAll')}
					desc={chats > 0 ? t('agent.deleteAllSub', { count: chats }) : t('agent.set.noChats')}
				>
					{#snippet control()}<button
							type="button"
							class="btn btn-danger-outline"
							disabled={chats === 0}
							onclick={ondeleteall}>{t('agent.set.deleteAsk')}</button
						>{/snippet}
				</SettingRow>
			</SettingGroup>
		{/if}
	</PageShell>
{:else}
	<PageShell
		title={titles[part]}
		crumbs={[{ label: t('agent.set.title'), href: '/agent?page=settings' }]}
		{phone}
		{onunfold}
	>
		<div class="sub">
			{#if part === 'speech'}
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
			{:else if part === 'initiative'}
				<InitiativeSettings part="initiative" />
			{:else if part === 'personality'}
				<InitiativeSettings part="mind" onrestart={onknow} />
			{:else if part === 'features'}
				<FeaturesPanel admin={status.admin} {calm} />
			{:else if part === 'push'}
				<p class="pushhint">{t('agent.set.pushHint')}</p>
				<PushDevices personal={!status.admin} />
			{:else if part === 'models'}
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
			{:else if part === 'gestures'}
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
							><strong>{t('agent.set.edge')}</strong><small>{t('agent.set.edgeHint')}</small></span
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
							><strong>{t('agent.lookVibrate')}</strong><small>{t('agent.set.device')}</small></span
						>
						<input type="checkbox" class="switch" bind:checked={look.vibrate} />
					</label>
				</div>
			{:else}
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

				<p class="cap">{t('agent.set.inChat')}</p>
				<div class="grp">
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
	</PageShell>
{/if}

<style>
	.sub {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-width: 720px;
	}
	.cap {
		margin: var(--space-3) 0 var(--space-1);
		font-size: var(--text-md);
		font-weight: 600;
		color: var(--text);
	}
	.pushhint,
	.hint {
		margin: 0 0 var(--space-2);
		line-height: 1.45;
		color: var(--muted);
	}
	.t small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	/* PLAN 78-5: the rows of a group stand together, no box around them. */
	.grp {
		display: flex;
		flex-direction: column;
		gap: 3px;
		flex-shrink: 0;
		min-width: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 48px;
		padding: var(--space-2-5) var(--space-4);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		background: var(--srow-bg);
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
		padding: var(--space-1-5) var(--space-2);
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
		gap: var(--space-2);
		margin-top: var(--space-3);
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
		padding: 0 var(--space-3);
	}
	.field {
		max-width: 46%;
	}
</style>
