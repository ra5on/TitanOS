<script lang="ts">
	// One container as a tile of the list (owner's design of 30 Oct 2026):
	// all tiles equally wide and 52 px high, app icon centred, name with
	// CPU and RAM below, the state as the stripe on the left (accent colour
	// while running). No buttons: the icon (or a right click) opens the menu
	// with Start/Stop, the addresses and all other actions.
	import type { DockerContainer } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, stateAge, webLinks, type ContainerActions } from './container.ts';
	import ContainerMenu from './ContainerMenu.svelte';

	type Props = ContainerActions & {
		container: DockerContainer;
		host: string;
		tailscaleHost: string;
		busy: boolean;
		/** A service inside a stack: named after the service. */
		member?: boolean;
		details: boolean;
	};
	let {
		container,
		host,
		tailscaleHost,
		busy,
		member = false,
		details,
		...actions
	}: Props = $props();

	let menu: ReturnType<typeof ContainerMenu> | undefined = $state();
	const name = $derived(dockerName(container, member ? 'service' : 'app'));
	const distinct = $derived(dockerName(container, 'distinct'));
	const running = $derived(isRunning(container));
	const links = $derived(webLinks(container, host, tailscaleHost));
	const age = $derived(stateAge(container));
	const cpu = $derived(running ? formatPercent(container.cpuPercent) : '–');
	const ram = $derived(running ? formatBytes(container.memoryBytes) : '–');
	const stateText = $derived(
		age
			? t(running ? 'docker.runningSince' : 'docker.stoppedSince', { time: age })
			: t(`docker.state.${container.state}`),
	);
</script>

<div
	id={`docker-container-${encodeURIComponent(container.name)}`}
	class="row"
	class:member
	class:stopped={!running}
	role="listitem"
	aria-label={name}
	title={details ? `${container.name} · ${container.image}` : container.name}
	oncontextmenu={(event) => menu?.openAt(event)}
>
	<button
		type="button"
		class="icon"
		aria-label={t('docker.actionsFor', { name: distinct })}
		aria-haspopup="menu"
		onpointerdown={() => menu?.press()}
		onclick={(event) => menu?.toggleUnder(event.currentTarget)}
		><AppIcon src={container.icon} {name} /></button
	>
	<span class="line">
		<span class="name">{name}</span>
		{#if container.updateAvailable}<span class="chip warn">{t('docker.update')}</span>{/if}
	</span>
	<!-- The stripe shows the state; the text is there for screen readers. -->
	<span class="values"
		><span class="sr-only">{stateText}, {t('docker.cpu')}</span>{cpu}<span class="sr-only"
			>, {t('docker.ram')}</span
		><span aria-hidden="true">{' · '}</span>{ram}</span
	>
	<ContainerMenu
		bind:this={menu}
		{container}
		name={distinct}
		{busy}
		{links}
		startStop
		trigger={false}
		{...actions}
	/>
</div>

<style>
	.row {
		display: grid;
		grid-template-columns: 30px minmax(0, 1fr);
		grid-template-rows: auto auto;
		align-content: center;
		column-gap: 10px;
		row-gap: 1px;
		height: 3.25rem;
		padding: 0 12px 0 14px;
		box-sizing: border-box;
		border: 1px solid var(--panel-border);
		border-radius: 14px;
		background: var(--panel-bg);
		-webkit-backdrop-filter: var(--panel-backdrop);
		backdrop-filter: var(--panel-backdrop);
		box-shadow:
			inset 4px 0 0 var(--accent),
			var(--panel-shadow);
		font-size: 0.875rem;
	}
	.row.stopped {
		box-shadow:
			inset 4px 0 0 var(--muted),
			var(--panel-shadow);
	}
	.icon {
		grid-row: 1 / span 2;
		align-self: center;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		padding: 0;
		overflow: hidden;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font-size: 0.75rem;
		font-weight: 600;
		cursor: pointer;
	}
	.icon :global(img) {
		padding: 0;
	}
	.icon:hover,
	.icon:focus-visible {
		outline: 2px solid color-mix(in srgb, var(--accent) 50%, transparent);
	}
	.stopped .icon:not(:hover) {
		opacity: 0.55;
	}
	.line {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 600;
	}
	.member .name {
		font-weight: 500;
	}
	.chip {
		flex: none;
		font-size: 0.6875rem;
	}
	.values {
		overflow: hidden;
		white-space: nowrap;
		color: var(--muted);
		font-family: var(--font-mono);
		font-size: 0.75rem;
	}
</style>
