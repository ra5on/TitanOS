<script lang="ts">
	import type { DockerContainer, VM, Share } from '#lib/api.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import {
		dockerGroups,
		stackPrimary,
		sanitizeDockerLayout,
		type DockerGroup,
	} from '#lib/docker-layout.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { vmOsIcon } from '#lib/vm-os.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import WorkloadMenu, { type WorkloadTarget } from '../WorkloadMenu.svelte';
	import VMDetails from '../VMDetails.svelte';
	import { positionGrid } from '../position-grid.svelte.ts';
	import type { TileId } from '../tiles.ts';
	import type { TileOptions } from '../options.ts';
	let {
		kind,
		options,
		containers = [],
		vms = [],
		shares = [],
		ready = false,
		error = '',
		admin = false,
		tailscaleHost = '',
		editing = false,
		kvm = false,
		disabled = false,
		standalone = false,
		onpin,
		onrefresh = () => {},
		onoption = () => {},
	}: {
		kind: 'docker' | 'vms' | 'shares';
		options: TileOptions;
		containers?: DockerContainer[];
		vms?: VM[];
		shares?: Share[];
		ready?: boolean;
		error?: string;
		admin?: boolean;
		tailscaleHost?: string;
		editing?: boolean;
		kvm?: boolean;
		disabled?: boolean;
		standalone?: boolean;
		onpin?: (id: TileId) => void;
		onrefresh?: () => void;
		onoption?: (key: keyof TileOptions, value: unknown) => void;
	} = $props();
	let expanded = $state<string[]>([]),
		details = $state(''),
		showAll = $state(false),
		width = $state(500);
	let menu = $state<{ target: WorkloadTarget; x: number; y: number } | null>(null);
	let shareMenu = $state<{ share: Share; x: number; y: number } | null>(null);
	const host = $derived(
		system.info?.network?.ipv4?.split('/')[0] || system.info?.hostname || 'nas',
	);
	const groups = $derived(
		(standalone
			? containers.map((c) => ({ id: `container:${c.name}`, name: c.name, members: [c] }))
			: dockerGroups(containers)
		).filter(
			(g) =>
				(options.items === null || options.items.includes(g.id)) &&
				(!options.runningOnly || g.members.some((c) => c.state === 'running')),
		),
	);
	const machines = $derived(
		vms.filter(
			(v) =>
				(options.items === null || options.items.includes(v.name)) &&
				(!options.runningOnly || v.state === 'running'),
		),
	);
	const folders = $derived(
		shares.filter((s) => options.items === null || options.items.includes(s.name)),
	);
	const total = $derived(
		kind === 'docker' ? groups.length : kind === 'vms' ? machines.length : folders.length,
	);
	const limit = $derived(showAll ? total : options.rows);
	const primaries = $derived(sanitizeDockerLayout(session.docker).primaryServices);
	const liveTarget = $derived.by(() => {
		const target = menu?.target;
		if (!target) return null;
		if (target.kind === 'vm') {
			const vm = vms.find((v) => v.name === target.vm.name);
			return vm ? { ...target, vm } : null;
		}
		if (target.stack) {
			const group = dockerGroups(containers).find((g) => g.name === target.name);
			return group ? groupTarget(group) : null;
		}
		const c = containers.find((c) => c.name === target.name);
		return c ? { ...target, members: [c], primary: c } : null;
	});
	function open(event: MouseEvent, target: WorkloadTarget) {
		if (editing) return;
		const r = (event.currentTarget as HTMLElement).getBoundingClientRect();
		menu = { target, x: r.left, y: r.bottom + 4 };
	}
	function toggle(id: string) {
		if (!editing)
			expanded = expanded.includes(id) ? expanded.filter((i) => i !== id) : [...expanded, id];
	}
	function groupTarget(group: DockerGroup): WorkloadTarget {
		return {
			kind: 'docker',
			name: group.name,
			members: group.members,
			stack: group.id.startsWith('app:'),
			primary: group.id.startsWith('app:') ? stackPrimary(group, primaries) : group.members[0],
		};
	}
	function lanes(node: HTMLElement) {
		return positionGrid(node, () => ({
			items: machines.slice(0, limit).map((vm) => ({ id: vm.name })),
			gap: 16,
			columns:
				options.list === 'grid'
					? Math.max(1, Math.floor((width + 16) / (options.entryWidth + 16)))
					: 1,
			onlayout: (_, w) => {
				if (width !== w) width = w;
			},
		}));
	}
</script>

{#if kind !== 'shares' && !admin}<p class="muted">{t('dashboard.workloadAdmin')}</p>
{:else if error}<p class="notice-warn" role="status">{t('ops.stale')} {error}</p>
	<button class="link" onclick={onrefresh}>{t('ui.retry')}</button>
{:else if !ready}<p class="muted">{t('common.loading')}</p>
{:else}
	{#if kind === 'shares'}
		<table class:grid={options.list === 'grid'}>
			<thead
				><tr
					><th>{t('dashboard.name')}</th><th>{t('nav.storage')}</th>{#if options.details}<th
							>{t('dashboard.services')}</th
						><th>{t('shares.access')}</th>{/if}</tr
				></thead
			>
			<tbody
				>{#each folders.slice(0, limit) as share (share.name)}<tr
						><td
							><button
								class="entry-name"
								data-edit-part="value"
								aria-label={t('dashboard.menu', { name: share.name })}
								aria-haspopup="menu"
								onclick={(e) => {
									if (editing) return;
									const r = e.currentTarget.getBoundingClientRect();
									shareMenu = { share, x: r.left, y: r.bottom };
								}}>{share.name}<Icon name="chevronDown" size={11} /></button
							></td
						><td>{share.available ? share.volume : t('storage.notMounted')}</td
						>{#if options.details}<td
								>{[share.smb ? 'SMB' : '', share.nfs?.enabled ? 'NFS' : '']
									.filter(Boolean)
									.join(' / ') || '–'}</td
							><td>{t(share.guest ? 'shares.guest' : 'dashboard.private')}</td>{/if}</tr
					>{:else}<tr><td colspan="4">{t('shares.empty')}</td></tr>{/each}</tbody
			>
		</table>
	{:else if kind === 'docker'}
		<div class="collection" class:grid={options.list === 'grid'}>
			{#each groups.slice(0, limit) as group (group.id)}{@const target =
					groupTarget(group)}{@const primary =
					target.kind === 'docker' ? target.primary : undefined}{@const stack =
					group.id.startsWith('app:')}{@const running = group.members.filter(
					(c) => c.state === 'running',
				).length}
				<div class="workload" class:expanded={stack && expanded.includes(group.id)}>
					<div class="entry">
						<button
							class="trigger"
							aria-label={t('dashboard.menu', { name: group.name })}
							aria-haspopup="menu"
							onclick={(e) => open(e, target)}
							><span class="app-icon"
								><AppIcon
									name={group.name}
									src={primary?.icon || group.members.find((c) => c.icon)?.icon}
								/></span
							><span class="identity"
								><span
									class="entry-name"
									title={stack ? group.name : dockerName(group.members[0], 'distinct')}
									data-edit-part="value"
									>{stack ? group.name : dockerName(group.members[0], 'distinct')}</span
								><span class="state" data-edit-part="labels"
									><i class:ok={running === group.members.length}></i>{stack && options.details
										? `${running} / ${group.members.length} ${t('dashboard.servicesRunning')}`
										: stack
											? t(running > 0 ? 'docker.state.running' : 'docker.state.exited')
											: tOr(`docker.state.${group.members[0].state}`, group.members[0].state)}</span
								></span
							></button
						>
						{#if stack}<button
								class="expand"
								aria-label={t('dashboard.stackContainers', { name: group.name })}
								aria-expanded={expanded.includes(group.id)}
								onclick={() => toggle(group.id)}
								><Icon
									name={expanded.includes(group.id) ? 'chevronDown' : 'arrowRight'}
									size={12}
								/></button
							>{/if}
					</div>
					{#if stack && expanded.includes(group.id)}<div class="members">
							{#each group.members as c}<button
									class="member"
									aria-haspopup="menu"
									aria-label={t('dashboard.menu', { name: c.name })}
									onclick={(e) =>
										open(e, {
											kind: 'docker',
											name: c.name,
											members: [c],
											primary: c,
											stack: false,
										})}
									><span class="member-icon"><AppIcon name={c.name} src={c.icon} /></span><span
										class="member-name">{c.name}</span
									><span>{tOr(`docker.state.${c.state}`, c.state)}</span></button
								>{/each}
						</div>{/if}
				</div>
			{:else}<p class="muted">{t('ops.none')}</p>{/each}
		</div>
	{:else}
		<div class="vm-lanes" {@attach lanes}>
			{#each machines.slice(0, limit) as vm (vm.name)}<div class="workload" data-position={vm.name}>
					<button
						class="trigger"
						aria-label={t('dashboard.menu', { name: vmLabel(vm) })}
						aria-haspopup="menu"
						onclick={(e) => open(e, { kind: 'vm', vm })}
						><span class="app-icon"
							><Icon name={vmOsIcon(vm)} size={Math.max(16, options.iconSize - 8)} /></span
						><span class="identity"
							><span class="entry-name" data-edit-part="value" title={vmLabel(vm)}
								>{vmLabel(vm)}</span
							><span class="state" data-edit-part="labels"
								><i class:ok={vm.state === 'running'}></i>{tOr(
									`vms.state.${vm.state}`,
									vm.state,
								)}</span
							></span
						></button
					>
					{#if details === vm.name}<VMDetails {vm} onclose={() => (details = '')} />{/if}
				</div>{/each}
		</div>
		{#if !machines.length}<p class="muted">{t('ops.none')}</p>{/if}
	{/if}
	{#if !standalone && ready && !error && !total && !options.runningOnly && kind !== 'shares'}<div
			class="footer"
		>
			<a href={kind === 'docker' ? '/apps' : '/vms'}
				>{t(kind === 'docker' ? 'dashboard.installApp' : 'dashboard.createVm')}<Icon
					name="arrowRight"
					size={12}
				/></a
			>
		</div>{:else if !standalone}<div class="footer">
			<span>{t('dashboard.visibleItems', { count: Math.min(total, limit), total })}</span
			>{#if total > limit}<button class="link" onclick={() => (showAll = true)}
					>{t('dashboard.showAll')}</button
				>{/if}
			{#if kind === 'docker' || kind === 'vms'}<label class="running-filter"
					><input
						type="checkbox"
						checked={options.runningOnly}
						disabled={editing || disabled}
						onchange={(e) => onoption('runningOnly', e.currentTarget.checked)}
					/>{t('dashboard.editor.runningOnly')}</label
				>{:else}<a href="/shares"
					>{t('dashboard.openSection')}<Icon name="arrowRight" size={12} /></a
				>{/if}
		</div>{/if}
{/if}
{#if menu && liveTarget && !editing}<WorkloadMenu
		target={liveTarget}
		x={menu.x}
		y={menu.y}
		{host}
		{tailscaleHost}
		{kvm}
		onclose={() => (menu = null)}
		{onrefresh}
		{onpin}
		ondetails={(name) => (details = details === name ? '' : name)}
		oncontainers={() => liveTarget?.kind === 'docker' && toggle(`app:${liveTarget.name}`)}
	/>{/if}
{#if shareMenu && !editing}{@const s = shareMenu.share}<ContextMenu
		label={t('dashboard.menu', { name: s.name })}
		x={shareMenu.x}
		y={shareMenu.y}
		onclose={() => (shareMenu = null)}
		><div class="menu-title">{s.name}</div>
		<a role="menuitem" href="/files?root={encodeURIComponent('share/' + s.name)}"
			><Icon name="folder" size={16} /><span>{t('dashboard.files')}</span></a
		><a role="menuitem" href="/shares?share={encodeURIComponent(s.name)}"
			><Icon name="edit" size={16} /><span>{t('dashboard.showDetails')}</span></a
		>{#if admin}<a role="menuitem" href="/shares?share={encodeURIComponent(s.name)}&tab=access"
				><Icon name="key" size={16} /><span>{t('dashboard.permissions')}</span></a
			>{/if}</ContextMenu
	>{/if}

<style>
	table.grid {
		display: block;
	}
	table.grid thead {
		position: absolute;
		clip-path: inset(50%);
		width: 1px;
		height: 1px;
		overflow: hidden;
	}
	table.grid tbody {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(210px, 100%), 1fr));
		gap: 12px;
	}
	table.grid tr {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		border: 1px solid var(--border);
		border-radius: 4px;
		padding: 10px;
		min-width: 0;
	}
	table.grid td {
		border: 0;
		padding: 0;
		min-width: 0;
	}
	table.grid td:first-child {
		flex-basis: 100%;
	}
	table.grid td:nth-child(2) {
		flex-basis: 100%;
		color: var(--muted);
	}
	table.grid td:nth-child(3) {
		flex: 1;
	}
	table.grid td:last-child {
		margin-left: auto;
	}

	.collection {
		display: flex;
		flex-direction: column;
		gap: var(--tile-row-gap, 10px);
		min-width: 0;
	}
	.collection.grid {
		display: grid;
		grid-template-columns: repeat(
			auto-fill,
			minmax(min(var(--tile-entry-width, 170px), 100%), 1fr)
		);
		gap: var(--tile-row-gap, 10px) 16px;
	}
	.workload {
		min-width: 0;
		padding: 3px 0;
	}
	.entry {
		display: flex;
		gap: 4px;
		min-width: 0;
		align-items: center;
	}
	.trigger {
		border: 0;
		background: transparent;
		padding: 4px;
		display: flex;
		align-items: center;
		gap: 9px;
		color: inherit;
		text-align: left;
		min-width: 0;
		width: 100%;
		border-radius: 4px;
		cursor: pointer;
		font: inherit;
		white-space: normal;
	}
	.trigger:hover,
	.trigger:focus-visible {
		background: var(--chip);
		outline: 1px solid var(--border);
	}
	.app-icon {
		width: var(--tile-icon-size, 32px);
		height: var(--tile-icon-size, 32px);
		flex: none;
		background: var(--chip);
		border-radius: 5px;
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--accent);
		font-size: var(--text-sm);
	}
	.identity {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
		gap: 3px;
	}
	.entry-name {
		display: -webkit-box;
		-webkit-box-orient: vertical;
		-webkit-line-clamp: var(--tile-name-lines, 2);
		line-clamp: var(--tile-name-lines, 2);
		overflow: hidden;
		min-width: 0;
		overflow-wrap: anywhere;
		font: var(--tile-value-weight, 400) var(--tile-value-size, var(--text-md))/1.35
			var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		text-align: var(--tile-align);
		white-space: normal;
	}
	.state {
		display: flex;
		gap: 5px;
		align-items: center;
		color: var(--tile-label-color, var(--muted));
		font-size: var(--tile-label-size, var(--text-sm));
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.state i {
		width: 5px;
		height: 5px;
		flex: none;
		background: var(--muted);
		border-radius: 0;
	}
	.state i.ok {
		background: var(--status-ok);
		border-radius: 50%;
	}
	.expand {
		padding: 3px;
		background: none;
		border: 0;
		color: var(--muted);
		cursor: pointer;
		flex: none;
	}

	.expanded .entry {
		max-width: 100%;
	}
	.members {
		display: grid;
		gap: 5px;
		border-top: 1px solid var(--border);
		padding: 8px 0 0 12px;
		margin-top: 8px;
	}
	.member {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 3px;
		border: 0;
		background: none;
		color: var(--muted);
		text-align: left;
		cursor: pointer;
		font-size: var(--tile-label-size, var(--text-sm));
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.member-name {
		flex: 1;
	}
	.member .member-icon {
		width: 24px;
		height: 24px;
		flex: none;
	}
	.member span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.footer {
		display: flex;
		justify-content: flex-start;
		gap: 10px;
		flex-wrap: wrap;
		align-items: center;
		font-size: var(--text-xs);
		color: var(--muted);
		margin-top: 12px;
	}
	.running-filter {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.vm-lanes {
		position: relative;
		min-width: 0;
	}
	.footer button {
		border: 0;
		background: none;
		padding: 0;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
	}
	.footer a {
		margin-left: auto;
	}
	.footer a {
		display: flex;
		gap: 5px;
		align-items: center;
		font-size: var(--text-sm);
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--tile-label-size, var(--text-sm));
	}
	th {
		font-weight: 400;
		color: var(--muted);
		text-align: left;
		padding: 0 6px 7px 0;
	}
	td {
		border-top: 1px solid var(--border);
		padding: 6px 6px 6px 0;
		overflow-wrap: anywhere;
	}
	td .entry-name {
		display: flex;
		align-items: center;
		gap: 5px;
		border: 0;
		background: none;
		padding: 0;
		cursor: pointer;
	}
</style>
