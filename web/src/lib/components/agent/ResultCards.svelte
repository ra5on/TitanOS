<script lang="ts" module>
	// Cards under an answer (0.95, owner 15 Nov 2026): what a step of the
	// turn read is shown as what it is – the containers with their state
	// and buttons, a device with its switch – instead of only as a table
	// the model wrote. The data is the result of the step itself (the same
	// the model saw); a result that cannot be read gives no card.
	import type { Entry } from '#lib/agent.ts';

	export type AppRow = {
		name: string;
		app: string;
		state: string;
		update: boolean;
		cpu: number;
		memory: number;
	};
	export type DeviceCard = { id: string; name: string; state: string; domain: string };

	function parse(result: string | undefined): unknown {
		if (!result) return null;
		const at = result.indexOf('{');
		if (at < 0) return null;
		try {
			return JSON.parse(result.slice(at));
		} catch {
			return null;
		}
	}

	/** The containers of the newest docker_list of a turn. */
	export function appRows(steps: Entry[]): AppRow[] {
		const step = steps.findLast((s) => s.role === 'tool' && s.tool === 'docker_list' && s.ok);
		const data = parse(step?.result) as { containers?: unknown } | null;
		if (!data || !Array.isArray(data.containers)) return [];
		return data.containers
			.filter((c): c is Record<string, unknown> => !!c && typeof c === 'object')
			.filter((c) => typeof c.name === 'string' && typeof c.state === 'string')
			.map((c) => ({
				name: c.name as string,
				app: typeof c.app === 'string' ? c.app : '',
				state: c.state as string,
				update: c.updateAvailable === true,
				cpu: typeof c.cpuPercent === 'number' ? c.cpuPercent : 0,
				memory: typeof c.memoryBytes === 'number' ? c.memoryBytes : 0,
			}))
			.sort(
				(a, b) =>
					Number(a.state === 'running') - Number(b.state === 'running') ||
					a.name.localeCompare(b.name),
			);
	}

	const SWITCHED = ['light', 'switch', 'fan', 'input_boolean', 'media_player'];

	/** The devices the turn read one by one (ha_state), each once, newest state. */
	export function deviceCards(steps: Entry[]): DeviceCard[] {
		const seen = new Map<string, DeviceCard>();
		for (const s of steps) {
			if (s.role !== 'tool' || s.tool !== 'ha_state' || !s.ok) continue;
			const data = parse(s.result) as Record<string, unknown> | null;
			const id = data && (data.entity_id ?? data.entityId ?? data.id);
			if (!data || typeof id !== 'string' || typeof data.state !== 'string') continue;
			const attributes = (data.attributes ?? {}) as Record<string, unknown>;
			const name =
				typeof data.name === 'string' && data.name
					? data.name
					: typeof attributes.friendly_name === 'string'
						? attributes.friendly_name
						: id;
			seen.set(id, { id, name, state: data.state, domain: id.split('.')[0] });
		}
		return [...seen.values()].filter((d) => SWITCHED.includes(d.domain)).slice(0, 6);
	}
</script>

<script lang="ts">
	import { formatBytes } from '#lib/format.ts';
	import { slide } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';
	import Icon from '#lib/components/Icon.svelte';
	import PhotoGrid, { photoHits } from './PhotoGrid.svelte';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		steps,
		active = false,
		onask,
	}: {
		steps: Entry[];
		/** Buttons only under the newest answer of a chat that can be written to. */
		active?: boolean;
		onask: (text: string) => void;
	} = $props();

	const apps = $derived(appRows(steps));
	const devices = $derived(deviceCards(steps));
	const photos = $derived(photoHits(steps));
	const running = $derived(apps.filter((a) => a.state === 'running').length);
	let all = $state(false);
	// The one row that shows its actions.
	let opened = $state('');
	type Act = { key: string; label: MessageKey; ask: MessageKey };
	/** What can be asked for a container, in the order it is offered. Each
	 *  one is a message to the agent, so its rights decide as always. */
	function actions(a: AppRow): Act[] {
		const act = (key: string): Act => ({
			key,
			label: `agent.card.${key}` as MessageKey,
			ask: `agent.card.ask.${key}` as MessageKey,
		});
		const list =
			a.state === 'running'
				? [act('restart'), act('stop'), act('logs'), act('details'), act('errors')]
				: [act('start'), act('logs'), act('details'), act('why')];
		if (a.update) list.unshift(act('updateNow'));
		return list;
	}
	const shown = $derived(all ? apps : apps.slice(0, 8));

	const STATES: Record<string, MessageKey> = {
		running: 'agent.card.running',
		exited: 'agent.card.stopped',
		created: 'agent.card.stopped',
		paused: 'agent.card.paused',
		restarting: 'agent.card.restarting',
		dead: 'agent.card.failed',
	};
	const stateWord = (s: string) => (STATES[s] ? t(STATES[s]) : s);
	const on = (d: DeviceCard) => !['off', 'unavailable', 'unknown', 'standby'].includes(d.state);
</script>

{#if apps.length}
	<section class="card" aria-label={t('agent.card.apps')}>
		<header>
			<strong>{t('agent.card.apps')}</strong>
			<span class="muted">{t('agent.card.appsCount', { running, total: apps.length })}</span>
		</header>
		<ul>
			{#each shown as a (a.name)}
				{@const on = a.state === 'running'}
				<li class:open={opened === a.name}>
					<!-- A row is one line; a tap opens what can be done with it
					     (owner 16 Nov 2026: more than restart and log, and the
					     list was long with two buttons under every name). -->
					{#snippet line()}
						<span class="lamp" data-state={on ? 'good' : 'off'} aria-hidden="true"></span>
						<span class="what">
							<span class="name">{a.name}</span>
							<span class="muted"
								>{stateWord(a.state)}{#if on && a.memory}
									· {formatBytes(a.memory)}{/if}{#if on && a.cpu >= 0.1}
									· {t('agent.card.cpu', {
										percent: a.cpu.toLocaleString(i18n.locale),
									})}{/if}{#if a.update}
									· {t('agent.card.update')}{/if}</span
							>
						</span>
						{#if active}<span class="chev" aria-hidden="true"
								><Icon name="chevronDown" size={16} /></span
							>{/if}
					{/snippet}
					{#if active}
						<button
							type="button"
							class="row"
							aria-expanded={opened === a.name}
							onclick={() => (opened = opened === a.name ? '' : a.name)}>{@render line()}</button
						>
					{:else}
						<div class="row">{@render line()}</div>
					{/if}
					{#if active && opened === a.name}
						<div class="do" transition:slide={{ duration: 260, easing: quintOut }}>
							{#each actions(a) as act (act.key)}
								<button
									type="button"
									class="btn"
									class:btn-primary={act.key === 'updateNow'}
									onclick={() => onask(t(act.ask, { name: a.name }))}>{t(act.label)}</button
								>
							{/each}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
		{#if apps.length > 8 && !all}
			<button type="button" class="more" onclick={() => (all = true)}
				>{t('agent.card.all', { count: apps.length })}</button
			>
		{/if}
	</section>
{/if}

<PhotoGrid {photos} />

{#each devices as d (d.id)}
	<section class="card device" aria-label={d.name}>
		<span class="lamp" data-state={on(d) ? 'warm' : 'off'} aria-hidden="true"></span>
		<span class="what">
			<span class="name">{d.name}</span>
			<span class="muted"
				>{t(
					on(d)
						? 'agent.card.on'
						: d.state === 'unavailable'
							? 'agent.card.gone'
							: 'agent.card.off',
				)}</span
			>
		</span>
		{#if active && d.state !== 'unavailable'}
			<button
				type="button"
				class="btn"
				onclick={() => onask(t(on(d) ? 'agent.card.askOff' : 'agent.card.askOn', { name: d.name }))}
				>{t(on(d) ? 'agent.card.switchOff' : 'agent.card.switchOn')}</button
			>
		{/if}
	</section>
{/each}

<style>
	.card {
		margin-top: var(--space-3);
		border: 1px solid var(--border);
		border-radius: 18px;
		background: var(--surface);
		overflow: hidden;
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-2);
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.device {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-2) var(--space-4);
	}
	li {
		border-top: 1px solid var(--border);
	}
	.row {
		width: 100%;
		min-height: 52px;
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-4);
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
	}
	button.row {
		cursor: pointer;
	}
	.chev {
		flex: none;
		color: var(--muted);
		transition: transform 0.22s cubic-bezier(0.22, 1, 0.36, 1);
	}
	li.open .chev {
		transform: rotate(180deg);
	}
	.device {
		padding-block: var(--space-3);
	}
	.lamp {
		flex: none;
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: var(--muted);
		opacity: 0.55;
	}
	.lamp[data-state='good'] {
		background: var(--status-good, #3fb96b);
		opacity: 1;
	}
	.lamp[data-state='warm'] {
		background: var(--ag-warm, var(--accent));
		opacity: 1;
		box-shadow: 0 0 8px color-mix(in srgb, var(--ag-warm, var(--accent)) 70%, transparent);
	}
	.what {
		flex: 1 1 12em;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.muted {
		color: var(--muted);
		font-size: 0.9em;
	}
	.do {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: 0 var(--space-4) var(--space-3);
	}
	.btn {
		min-height: 36px;
		border-radius: 999px;
	}
	.more {
		width: 100%;
		min-height: 44px;
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--ag-link, var(--accent));
		font: inherit;
		cursor: pointer;
	}
</style>
