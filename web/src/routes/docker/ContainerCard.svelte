<script lang="ts">
	// One container as a card of fixed shape: everything is visible at once,
	// so nothing unfolds and the grid keeps its place.
	import type { DockerContainer } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, portLabels, stateAge, webLinks, type ContainerActions } from './container.ts';
	import ContainerMenu from './ContainerMenu.svelte';
	import CpuSpark from './CpuSpark.svelte';

	type Props = ContainerActions & {
		container: DockerContainer;
		host: string;
		tailscaleHost: string;
		busy: boolean;
		details: boolean;
	};
	let { container, host, tailscaleHost, busy, details, ...actions }: Props = $props();

	const MAX_PORTS = 3;
	const name = $derived(dockerName(container));
	const running = $derived(isRunning(container));
	const links = $derived(webLinks(container, host, tailscaleHost));
	const ports = $derived(portLabels(container));
	const age = $derived(stateAge(container));
</script>

<article class="tile card" class:stopped={!running} aria-label={name}>
	<div class="top">
		<span class="icon"><AppIcon src={container.icon} {name} /></span>
		<div class="names">
			<span class="line">
				{#if container.managed}<a
						class="name"
						href="/docker/apps/{container.app}"
						title={container.name}>{name}</a
					>{:else}<span class="name" title={container.name}>{name}</span>{/if}
				{#if container.updateAvailable}<span class="chip warn">{t('docker.update')}</span>{/if}
			</span>
			{#if details}<span class="mono image" title={container.image}>{container.image}</span>{/if}
		</div>
		<ContainerMenu {container} name={dockerName(container, 'distinct')} {busy} {...actions} />
	</div>

	<div class="stats">
		<div
			class="state"
			title={age
				? t(running ? 'docker.runningSince' : 'docker.stoppedSince', { time: age })
				: undefined}
		>
			<span class="dot" class:ok={running} aria-hidden="true"></span>
			<span>{t(`docker.state.${container.state}`)}</span>
			<span class="age">{age}</span>
		</div>
		<div class="value">
			<span class="label">{t('docker.cpu')}</span><span class="mono"
				>{running ? formatPercent(container.cpuPercent) : '–'}</span
			>
		</div>
		<div class="value">
			<span class="label">{t('docker.ram')}</span><span class="mono"
				>{running ? formatBytes(container.memoryBytes) : '–'}</span
			>
		</div>
	</div>
	{#if details}<div class="spark">
			{#if running}<CpuSpark values={container.cpuHistory} />{/if}
		</div>{/if}

	<div class="bottom">
		<span class="ports">
			{#each ports.slice(0, MAX_PORTS) as port (port)}<span class="chip mono">{port}</span>{/each}
			{#if ports.length > MAX_PORTS}<span
					class="chip mono"
					title={ports.slice(MAX_PORTS).join(', ')}>+{ports.length - MAX_PORTS}</span
				>{/if}
		</span>
		<button
			type="button"
			class="btn small"
			onclick={() => actions.onaction(running ? 'stop' : 'start')}
			disabled={busy}
		>
			<Icon name={running ? 'stop' : 'play'} size={14} />{t(
				running ? 'docker.stop' : 'docker.start',
			)}
		</button>
	</div>
	{#if links.length > 0}
		<div class="links">
			{#each links as link, index (`${index}:${link.href}`)}<a
					class="btn small"
					href={link.href}
					target="_blank"
					rel="noreferrer"
					title={link.label}><Icon name={link.icon} size={14} />{link.text}</a
				>{/each}
		</div>
	{/if}
</article>

<style>
	.card {
		position: relative;
		height: 100%;
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 12px 14px;
		font-size: 0.875rem;
	}
	.top {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.icon {
		width: 36px;
		height: 36px;
		flex: none;
		overflow: hidden;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font-size: 0.875rem;
		font-weight: 600;
	}
	.stopped .icon {
		opacity: 0.5;
	}
	.names {
		flex: 1 1 auto;
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
	.stats {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 4.5rem 5rem;
		align-items: end;
		gap: 12px;
	}
	.state {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		white-space: nowrap;
	}
	.age {
		overflow: hidden;
		text-overflow: ellipsis;
		color: var(--muted);
		font-size: 0.75rem;
	}
	.value {
		display: flex;
		flex-direction: column;
		text-align: right;
	}
	.value .mono {
		font-size: 0.8125rem;
	}
	.stopped .value .mono {
		color: var(--muted);
	}
	.label {
		color: var(--muted);
		font-size: 0.6875rem;
	}
	.spark {
		height: 22px;
	}
	.bottom {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: auto;
		padding-top: 10px;
		border-top: 1px solid var(--panel-border);
	}
	.ports {
		flex: 1 1 auto;
		display: flex;
		gap: 4px;
		min-width: 0;
		overflow: hidden;
	}
	.links {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.ports .chip {
		flex: none;
		font-size: 0.75rem;
	}
	.small {
		flex: none;
		min-height: 2rem;
		height: 2rem;
		padding: 0 0.625rem;
		gap: 6px;
		font-size: 0.8125rem;
	}
	@media (max-width: 760px) {
		.small {
			min-height: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
