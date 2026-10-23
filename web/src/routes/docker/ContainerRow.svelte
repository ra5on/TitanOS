<script lang="ts">
	// One container as a row of the list. The columns come from the list
	// (subgrid), so that values line up across all rows and stacks.
	import type { DockerContainer } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, monogramHue, portLabels, stateAge, webLinks, type ContainerActions } from './container.ts';
	import ContainerMenu from './ContainerMenu.svelte';
	import CpuSpark from './CpuSpark.svelte';

	type Props = ContainerActions & {
		container: DockerContainer;
		host: string;
		tailscaleHost: string;
		busy: boolean;
		/** A service inside a stack: named after the service, without an icon. */
		member?: boolean;
		details: boolean;
	};
	let { container, host, tailscaleHost, busy, member = false, details, ...actions }: Props = $props();

	const MAX_PORTS = 3;
	const name = $derived(dockerName(container, member ? 'service' : 'app'));
	const running = $derived(isRunning(container));
	const links = $derived(webLinks(container, host, tailscaleHost));
	const ports = $derived(portLabels(container));
	const age = $derived(stateAge(container));
</script>

<div id={`docker-container-${encodeURIComponent(container.name)}`} class="row" class:member class:stopped={!running} role="listitem" aria-label={name}>
	<div class="who">
		{#if !member}
			<span class="icon" style:background="hsl({monogramHue(container.name)} 45% 42%)"><AppIcon src={container.icon} {name} /></span>
		{/if}
		<span class="names">
			<span class="line">
				{#if container.managed && !member}<a class="name" href="/docker/apps/{container.app}" title={container.name}>{name}</a>{:else}<span class="name" title={container.name}>{name}</span>{/if}
				{#if container.updateAvailable}<span class="chip warn">{t('docker.update')}</span>{/if}
			</span>
			{#if details}<span class="mono image" title={container.image}>{container.image}</span>{/if}
		</span>
	</div>
	<div class="stats">
	<div class="state" title={age ? t(running ? 'docker.runningSince' : 'docker.stoppedSince', { time: age }) : undefined}>
		<span class="dot" class:ok={running} aria-hidden="true"></span>
		<span>{t(`docker.state.${container.state}`)}</span>
		<span class="age">{age}</span>
	</div>
	<div class="cpu">
		<span class="label">{t('docker.cpu')}</span>
		<span class="mono">{running ? formatPercent(container.cpuPercent) : '–'}</span>
		{#if details}<span class="spark">{#if running}<CpuSpark values={container.cpuHistory} />{/if}</span>{/if}
	</div>
	<div class="ram">
		<span class="label">{t('docker.ram')}</span>
		<span class="mono">{running ? formatBytes(container.memoryBytes) : '–'}</span>
	</div>
	</div>
	<div class="ports">
		{#each ports.slice(0, MAX_PORTS) as port (port)}<span class="chip mono">{port}</span>{/each}
		{#if ports.length > MAX_PORTS}<span class="chip mono" title={ports.slice(MAX_PORTS).join(', ')}>+{ports.length - MAX_PORTS}</span>{/if}
	</div>
	<!-- The addresses get a labelled line of their own in the stacked layout. -->
	{#if links.length > 0}
		<div class="links">
			{#each links as link (link.href)}<a class="btn small" href={link.href} target="_blank" rel="noreferrer" title={link.label}><Icon name={link.icon} size={14} />{link.text}</a>{/each}
		</div>
	{/if}
	<div class="acts">
		{#each links as link (link.href)}<a class="btn small link" href={link.href} target="_blank" rel="noreferrer" aria-label={link.label} title={link.label}><Icon name={link.icon} size={14} /><span class="text">{link.text}</span></a>{/each}
		<button type="button" class="btn small toggle" onclick={() => actions.onaction(running ? 'stop' : 'start')} disabled={busy} aria-label={t(running ? 'docker.stop' : 'docker.start')}>
			<Icon name={running ? 'stop' : 'play'} size={14} /><span class="text">{t(running ? 'docker.stop' : 'docker.start')}</span>
		</button>
		<ContainerMenu {container} name={dockerName(container, 'distinct')} {busy} {...actions} />
	</div>
</div>

<style>
	.row {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: subgrid;
		align-items: center;
		min-height: 3.5rem;
		padding: 8px var(--tile-pad);
		border-top: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.row:hover {
		background: color-mix(in srgb, var(--chip) 45%, transparent);
	}
	.member .who {
		padding-left: 44px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
	}
	.icon {
		width: 32px;
		height: 32px;
		flex: none;
		overflow: hidden;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font-size: 0.8125rem;
		font-weight: 600;
	}
	.stopped .icon {
		opacity: 0.5;
	}
	.names {
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 1px;
	}
	.line {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--text);
		font-weight: 600;
		text-decoration: none;
	}
	.member .name {
		font-weight: 500;
	}
	a.name:hover {
		text-decoration: underline;
	}
	.line .chip {
		flex: none;
		font-size: 0.6875rem;
	}
	.image {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--muted);
		font-size: 0.75rem;
	}
	.state {
		display: flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}
	.age {
		color: var(--muted);
		font-size: 0.75rem;
	}
	.cpu {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}
	.cpu .mono {
		flex: none;
		min-width: 3.25rem;
	}
	.spark {
		flex: 1 1 auto;
		max-width: 5.5rem;
		height: 20px;
	}
	.ram .mono,
	.cpu .mono {
		font-size: 0.8125rem;
	}
	.stopped .cpu .mono,
	.stopped .ram .mono {
		color: var(--muted);
	}
	/* Visible in the stacked layout only; the list header names the columns. */
	.label {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
	.ports {
		display: flex;
		flex-wrap: nowrap;
		gap: 4px;
		min-width: 0;
		overflow: hidden;
	}
	.ports .chip {
		flex: none;
		font-size: 0.75rem;
	}
	.acts {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 6px;
	}
	.small {
		min-height: 2rem;
		height: 2rem;
		padding: 0 0.625rem;
		gap: 6px;
		font-size: 0.8125rem;
	}
	.toggle {
		min-width: 5.25rem;
	}

	.links {
		display: none;
	}
	/* With several addresses the buttons would crowd out the names. */
	@container docker-list (max-width: 1180px) {
		.link {
			min-width: 2rem;
			padding: 0;
		}
		.link .text {
			display: none;
		}
	}

	/* The values are cells of the list's columns; the wrapper only counts
	   in the stacked layout. */
	.stats {
		display: contents;
	}

	@container docker-list (max-width: 760px) {
		.row {
			grid-template-columns: minmax(0, 1fr) auto;
			grid-template-areas:
				'who acts'
				'stats stats'
				'ports ports'
				'links links';
			gap: 6px 12px;
			padding: 10px 12px;
		}
		.who {
			grid-area: who;
		}
		.member .who {
			padding-left: 0;
		}
		.stats {
			grid-area: stats;
			display: flex;
			flex-wrap: wrap;
			align-items: center;
			gap: 4px 16px;
		}
		.cpu,
		.ram {
			display: flex;
			gap: 6px;
		}
		.label {
			position: static;
			width: auto;
			height: auto;
			clip-path: none;
			color: var(--muted);
			font-size: 0.75rem;
		}
		.cpu .mono {
			min-width: 0;
		}
		.spark {
			display: none;
		}
		.ports {
			grid-area: ports;
			flex-wrap: wrap;
		}
		.ports:empty {
			display: none;
		}
		.links {
			grid-area: links;
			display: flex;
			flex-wrap: wrap;
			gap: 6px;
		}
		.links .small {
			min-width: 0;
			padding: 0 0.75rem;
		}
		.links .small :global(svg) {
			flex: none;
		}
		.acts .link {
			display: none;
		}
		.acts {
			grid-area: acts;
			gap: 4px;
		}
		.small {
			min-width: 2.75rem;
			min-height: 2.75rem;
			height: 2.75rem;
			padding: 0;
		}
		.small .text {
			display: none;
		}
	}
</style>
