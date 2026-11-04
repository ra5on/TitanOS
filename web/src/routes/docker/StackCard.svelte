<script lang="ts">
	// A Compose stack as one card of the same size as a container card
	// (owner's decision of 4 Nov 2026, variant B): running/total, the CPU
	// and RAM of the stack, its combined CPU curve, the services as chips,
	// "Services" unfolds them as cards right after this one.
	import type { DockerContainer } from '#lib/api.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { stackCpuHistory, type DockerGroup } from '#lib/docker-layout.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, webLinks } from './container.ts';
	import CpuSpark from './CpuSpark.svelte';

	type Props = {
		group: DockerGroup;
		/** The service whose addresses the stack shows (see "Links"). */
		primary: DockerContainer | undefined;
		host: string;
		tailscaleHost: string;
		busy: boolean;
		details: boolean;
		open: boolean;
		/** The id of the services shown after the card (aria-controls). */
		drawer: string;
		ontoggle: () => void;
		/** The stack menu under an anchor, or at the mouse for a right click. */
		onmenu: (anchor: HTMLElement) => void;
		onmenuat: (event: MouseEvent) => void;
		onmenupress: () => void;
		onstartall: () => void;
		onstopall: () => void;
	};
	let {
		group,
		primary,
		host,
		tailscaleHost,
		busy,
		details,
		open,
		drawer,
		ontoggle,
		onmenu,
		onmenuat,
		onmenupress,
		onstartall,
		onstopall,
	}: Props = $props();

	const total = $derived(group.members.length);
	const running = $derived(group.members.filter(isRunning));
	const sum = (value: (container: DockerContainer) => number) =>
		running.reduce((all, container) => all + value(container), 0);
	const icon = $derived(group.members.find((container) => container.icon)?.icon ?? '');
	const links = $derived(primary ? webLinks(primary, host, tailscaleHost) : []);
	const history = $derived(stackCpuHistory(running));
	const update = $derived(group.members.some((container) => container.updateAvailable));
	// One stopped service is enough to offer starting all of them.
	const stopped = $derived(running.length < total);
</script>

<article
	class="tile card"
	class:stopped={running.length === 0}
	aria-label="{t('docker.stack')} {group.name}"
	oncontextmenu={(event) => {
		event.preventDefault();
		onmenuat(event);
	}}
>
	<div class="top">
		<button
			type="button"
			class="icon"
			aria-label={t('docker.actionsFor', { name: group.name })}
			aria-haspopup="menu"
			onpointerdown={onmenupress}
			onclick={(event) => onmenu(event.currentTarget)}
			><AppIcon src={icon} name={group.name} /></button
		>
		<div class="names">
			<span class="line"
				><span class="name" title={group.name}>{group.name}</span>{#if update}<span
						class="chip warn">{t('docker.update')}</span
					>{/if}</span
			>
			<span class="sub">{t('docker.stackKind', { count: total })}</span>
		</div>
		<button
			type="button"
			class="more"
			aria-label={t('docker.actionsFor', { name: group.name })}
			title={t('docker.actionsFor', { name: group.name })}
			aria-haspopup="menu"
			onpointerdown={onmenupress}
			onclick={(event) => onmenu(event.currentTarget)}
		>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
				><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle
					cx="19"
					cy="12"
					r="1.8"
				/></svg
			>
		</button>
	</div>

	<div class="stats">
		<div class="state">
			<span
				class="dot"
				class:ok={running.length === total}
				class:warn={running.length > 0 && running.length < total}
				aria-hidden="true"
			></span>
			<span>{t('docker.stackRunning', { running: running.length, total })}</span>
		</div>
		<div class="value">
			<span class="label">{t('docker.cpu')}</span><span class="mono"
				>{running.length ? formatPercent(sum((c) => c.cpuPercent)) : '–'}</span
			>
		</div>
		<div class="value">
			<span class="label">{t('docker.ram')}</span><span class="mono"
				>{running.length ? formatBytes(sum((c) => c.memoryBytes)) : '–'}</span
			>
		</div>
	</div>
	{#if details}<div class="spark">
			{#if running.length}<CpuSpark values={history} />{/if}
		</div>{/if}

	<ul class="services" aria-label={t('docker.servicesOf', { name: group.name })}>
		{#each group.members as container (container.name)}
			{@const up = isRunning(container)}
			<li class="service" class:stopped={!up}>
				<span class="dot" class:ok={up} aria-hidden="true"></span>{dockerName(
					container,
					'service',
				)}<span class="sr-only">: {t(`docker.state.${container.state}`)}</span
				>{#if container.updateAvailable}<span class="arrow" title={t('docker.update')}
						><Icon name="arrowUp" size={12} /><span class="sr-only">, {t('docker.update')}</span
						></span
					>{/if}
			</li>
		{/each}
	</ul>

	<div class="bottom">
		<button
			type="button"
			class="btn small"
			aria-expanded={open}
			aria-controls={open ? drawer : undefined}
			onclick={ontoggle}
			><span class="chevron" class:folded={!open}><Icon name="chevronDown" size={14} /></span>{t(
				'docker.services',
			)}</button
		>
		<button
			type="button"
			class="btn small start-stop"
			disabled={busy}
			onclick={stopped ? onstartall : onstopall}
			><Icon name={stopped ? 'play' : 'stop'} size={14} />{t(
				stopped ? 'docker.startAll' : 'docker.stopAll',
			)}</button
		>
	</div>
	{#if links.length > 0}
		<div class="actions">
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
	/* The same shape as ContainerCard. */
	.card {
		position: relative;
		height: 100%;
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: var(--space-3) 14px;
		font-size: var(--text-md);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.icon {
		padding: 0;
		border: 0;
		background: none;
		cursor: pointer;
		width: 36px;
		height: 36px;
		flex: none;
		overflow: hidden;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font-size: var(--text-md);
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
		gap: var(--space-2);
		min-width: 0;
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 600;
	}
	.line .chip {
		flex: none;
		font-size: var(--text-xs);
	}
	.sub {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.more {
		display: inline-flex;
		flex: none;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.more:hover {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}
	.stats {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 4.5rem 5rem;
		align-items: end;
		gap: var(--space-3);
	}
	.state {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		white-space: nowrap;
	}
	.value {
		display: flex;
		flex-direction: column;
		text-align: right;
	}
	.value .mono {
		font-size: var(--text-base);
	}
	.stopped .value .mono {
		color: var(--muted);
	}
	.label {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.spark {
		height: 22px;
	}
	.services {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.service {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		max-width: 100%;
		padding: 2px var(--space-2);
		overflow: hidden;
		border: 1px solid var(--panel-border);
		border-radius: 999px;
		font-size: var(--text-sm);
		white-space: nowrap;
		text-overflow: ellipsis;
	}
	.service.stopped {
		color: var(--muted);
	}
	.arrow {
		display: inline-flex;
		color: var(--warn-text);
	}
	.bottom {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: auto;
		padding-top: 10px;
		border-top: 1px solid var(--panel-border);
	}
	.start-stop {
		margin-left: auto;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 6px;
	}
	.chevron {
		display: inline-flex;
		transition: transform 0.15s ease;
	}
	.chevron.folded {
		transform: rotate(-90deg);
	}
	.small {
		flex: none;
		min-height: 2rem;
		height: 2rem;
		padding: 0 0.625rem;
		gap: 6px;
		font-size: var(--text-base);
	}
	@media (prefers-reduced-motion: reduce) {
		.chevron {
			transition: none;
		}
	}
	@media (max-width: 760px) {
		.small,
		.more {
			min-height: 2.75rem;
			height: 2.75rem;
		}
		.more {
			width: 2.75rem;
		}
	}
</style>
