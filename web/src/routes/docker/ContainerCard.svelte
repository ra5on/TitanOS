<script lang="ts">
	import { dockerName } from '#lib/docker-name.ts';
	import { appURL, publicURL } from '#lib/app-links.ts';
 import AppIcon from '#lib/components/AppIcon.svelte';
 import type { DockerContainer } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDuration, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		container: DockerContainer;
		host: string;
 tailscaleHost?: string;
		busy: boolean;
		/** One row inside a stack instead of a card of its own. */
		compact?: boolean;
		onaction: (action: 'start' | 'stop' | 'restart') => void;
		onlogs: () => void;
		onremove: () => void;
		onconsole: () => void;
		onupdate: () => void;
	};
	let { container, host, tailscaleHost = '', busy, compact = false, onaction, onlogs, onremove, onconsole, onupdate }: Props = $props();

	const displayName = $derived(dockerName(container));
	const running = $derived(container.state === 'running' || container.state === 'restarting');
	let menu = $state(false);
	let root: HTMLElement | undefined = $state();

	// Without an icon from a template, an app gets a monogram in a color of
	// its own, derived from its name so that it stays the same.
	const hue = $derived([...container.name].reduce((sum, char) => (sum * 31 + char.charCodeAt(0)) % 360, 7));

	const since = $derived.by(() => {
		const time = new Date(container.since).getTime();
		if (!time || time < 0) return '';
		const seconds = Math.max(0, (Date.now() - time) / 1000);
		return t(running ? 'docker.runningSince' : 'docker.stoppedSince', { time: formatDuration(seconds) });
	});

	/** The CPU history as points of a small line chart, scaled to its own peak. */
	const spark = $derived.by(() => {
		const values = container.cpuHistory;
		if (values.length < 2) return '';
		const peak = Math.max(...values, 1);
		return values.map((value, index) => `${((index / (values.length - 1)) * 100).toFixed(1)},${(22 - (value / peak) * 20).toFixed(1)}`).join(' ');
	});

 const webui = $derived(appURL(container.webui, host));
 const tailscaleURL = $derived(appURL(container.webui, tailscaleHost, true));
 const externalURL = $derived(publicURL(container.publicUrl ?? ''));

	function choose(action: () => void) {
		menu = false;
		action();
	}
</script>

<svelte:window onclick={(event) => menu && !root?.contains(event.target as Node) && (menu = false)} onkeydown={(event) => event.key === 'Escape' && (menu = false)} />

{#snippet links()}
 {#if webui}<a class="btn small" href={webui} target="_blank" rel="noreferrer">{t('docker.webui')}</a>{/if}
 {#if tailscaleURL}<a class="btn small" href={tailscaleURL} target="_blank" rel="noreferrer">{t('docker.tailscaleWebui')}</a>{/if}
 {#if externalURL}<a class="btn small" href={externalURL} target="_blank" rel="noreferrer">{t('docker.publicWebui')}</a>{/if}
{/snippet}
{#snippet ports()}
	{#each container.ports as port (`${port.host}/${port.protocol}`)}
        <span class="chip mono">{port.host}{port.protocol === 'udp' ? '/udp' : ''}</span>
	{/each}
{/snippet}

{#snippet actions()}
	{#if menu}
		<div class="menu" role="menu" aria-label={t('docker.actionsFor', { name: dockerName(container, compact ? 'service' : 'app') })}>
			<button type="button" role="menuitem" onclick={() => choose(onlogs)}>{t('docker.logs')}</button>
			<button type="button" role="menuitem" onclick={() => choose(onconsole)} disabled={!running}>{t('docker.console')}</button>
			{#if container.managed}
				<a role="menuitem" href="/docker/apps/{container.app}">{t('docker.edit')}</a>
			{/if}
			<button type="button" role="menuitem" onclick={() => choose(() => onaction('restart'))} disabled={busy}>{t('docker.restart')}</button>
			{#if container.managed && container.updateAvailable}
				<button type="button" role="menuitem" onclick={() => choose(onupdate)} disabled={busy}>{t('docker.updateNow')}</button>
			{/if}
			<hr />
			<button type="button" role="menuitem" onclick={() => choose(onremove)}>{t('docker.remove')}</button>
		</div>
	{/if}
{/snippet}

{#if compact}
	<div class="member" bind:this={root}>
		<span class="dot" class:ok={running} aria-hidden="true"></span>
		<span class="names">
			<span class="line"><span class="name">{dockerName(container, 'service')}</span>{#if container.updateAvailable}<span class="chip warn">{t('docker.update')}</span>{/if}</span>
			<span class="mono image">{container.image}</span>
		</span>
		<span class="ports">{@render ports()}{@render links()}</span>
		<span class="mono value">{running ? formatPercent(container.cpuPercent) : '–'}</span>
		<span class="mono value">{running ? formatBytes(container.memoryBytes) : '–'}</span>
		<button type="button" class="btn small" onclick={() => onaction(running ? 'stop' : 'start')} disabled={busy}>{t(running ? 'docker.stop' : 'docker.start')}</button>
		<button type="button" class="more" aria-label={t('docker.actionsFor', { name: dockerName(container, compact ? 'service' : 'app') })} aria-expanded={menu} onclick={() => (menu = !menu)}>…</button>
		{@render actions()}
	</div>
{:else}
	<article class="tile" bind:this={root} aria-label={displayName}>
		<div class="top">
			<button type="button" class="monogram" style:background="hsl({hue} 45% 42%)" aria-label={t('docker.actionsFor', { name: dockerName(container, compact ? 'service' : 'app') })} aria-expanded={menu} onclick={() => (menu = !menu)}><AppIcon src={container.icon} name={displayName} /> </button>
			<div class="names">
				<div class="line">
					<span class="name" title={container.name}>{displayName}</span>
					<span class="state"><span class="dot" class:ok={running} aria-hidden="true"></span>{t(`docker.state.${container.state}`)}</span>
					{#if container.updateAvailable}<span class="chip warn">{t('docker.update')}</span>{/if}
				</div>
				<span class="mono image">{container.image}</span>
			</div>
			<button type="button" class="more" aria-label={t('docker.actionsFor', { name: dockerName(container, compact ? 'service' : 'app') })} aria-expanded={menu} onclick={() => (menu = !menu)}>…</button>
		</div>
		<div class="stats">
			<div><span class="label">{t('docker.cpu')}</span><span class="mono">{running ? formatPercent(container.cpuPercent) : '–'}</span></div>
			<svg viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true">{#if running && spark}<polyline points={spark} />{/if}</svg>
			<div><span class="label">{t('docker.ram')}</span><span class="mono">{running ? formatBytes(container.memoryBytes) : '–'}</span></div>
		</div>
		{#if container.ports.length > 0}
			<div class="ports">{@render ports()}</div>
		{/if}
		<div class="bottom">
			<span class="muted since">{since}</span>
			{@render links()}
			<button type="button" class="btn small" onclick={() => onaction(running ? 'stop' : 'start')} disabled={busy}>
				<Icon name={running ? 'pause' : 'play'} size={14} />{t(running ? 'docker.stop' : 'docker.start')}
			</button>
		</div>
		{@render actions()}
	</article>
{/if}

<style>
	article {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 10px 12px;
	}
	.top {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.monogram {
		width: 36px;
		height: 36px;
		flex: none;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font: inherit;
		font-size: 0.9375rem;
		font-weight: 600;
		cursor: pointer;
	}
	.names {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 10px;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.image {
		font-size: 0.75rem;
		color: var(--muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.more {
		width: 36px;
		height: 36px;
		flex: none;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: 1.125rem;
		cursor: pointer;
	}
	.more:hover {
		background: var(--chip);
		color: var(--text);
	}
	.stats {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: 12px;
		font-size: 0.875rem;
	}
	.stats div {
		display: flex;
		flex-direction: column;
	}
	.label {
		font-size: 0.6875rem;
		color: var(--muted);
	}
	svg {
		width: 100%;
		height: 24px;
	}
	polyline {
		fill: none;
		stroke: var(--accent);
		stroke-width: 1.5;
		vector-effect: non-scaling-stroke;
	}
	.ports {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.ports a {
		color: var(--text);
		text-decoration: none;
	}
	.ports a:hover {
		border-color: var(--accent);
	}
	.bottom {
		display: flex;
 flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.since {
		margin-right: auto;
		font-size: 0.75rem;
	}
	.small {
		height: 2.125rem;
		padding: 0 0.75rem;
		font-size: 0.8125rem;
	}
	.member {
		position: relative;
		display: grid;
		grid-template-columns: 8px minmax(0, 1fr) auto 4.5rem 5.5rem auto 36px;
		align-items: center;
		gap: 12px;
		padding: 10px 16px;
		border-top: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.value {
		text-align: right;
		font-size: 0.8125rem;
	}
	.menu {
		position: absolute;
		right: 12px;
		top: 56px;
		z-index: 10;
		width: 200px;
		display: flex;
		flex-direction: column;
		padding: 6px;
		background: var(--menu-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		-webkit-backdrop-filter: var(--panel-backdrop);
		backdrop-filter: var(--panel-backdrop);
		box-shadow: var(--shadow-menu);
	}
	.member .menu {
		top: 44px;
	}
	.menu button,
	.menu a {
		display: flex;
		align-items: center;
		min-height: 38px;
		padding: 0 10px;
		background: transparent;
		border: 0;
		text-decoration: none;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.menu button:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.menu a:hover,
	.menu button:hover:not(:disabled) {
		background: var(--chip);
	}
	.menu hr {
		height: 1px;
		margin: 6px 4px;
		border: 0;
		background: var(--border);
	}
 @media (min-width: 1000px) {
 article { display: grid; grid-template-columns: minmax(220px, 1fr) 160px minmax(220px, auto); column-gap: 20px; align-items: center; }
 .top { grid-column: 1; grid-row: 1; }
 article > .ports { grid-column: 1; grid-row: 2; padding-left: 44px; }
 .stats { grid-column: 2; grid-row: 1 / 3; gap: 8px; }
 .bottom { grid-column: 3; grid-row: 1 / 3; justify-content: flex-end; max-width: 30rem; }
 .bottom .since { flex-basis: 100%; text-align: right; }
 }
 @media (min-width: 761px) and (max-width: 999px) {
 article { display: grid; grid-template-columns: minmax(0, 1fr) 160px; }
 .top { grid-column: 1; }
 .stats { grid-column: 2; grid-row: 1; }
 article > .ports, .bottom { grid-column: 1 / -1; }
 }
 @media (max-width: 760px) {
 .member { grid-template-columns: 8px minmax(0, 1fr) auto; gap: 8px; padding: var(--tile-pad); }
 .member .ports { grid-column: 2 / -1; }
 .member .value { text-align: left; grid-column: 2; }
 .member > .more { grid-column: 3; grid-row: 1; }
 .member > .btn { grid-column: 3; grid-row: 3 / 5; }
 .bottom .since { flex-basis: 100%; }
 .small, .more { min-height: 44px; }
 }
</style>
