<script lang="ts">
	import type { DockerContainer, VM, Share } from '#lib/api.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { dockerGroups, stackPrimary, sanitizeDockerLayout } from '#lib/docker-layout.ts';
	import { webLinks } from '../../../routes/docker/container.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
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
	} = $props();
	let expanded = $state<string[]>([]);
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	const items = $derived(
		kind === 'docker'
			? dockerGroups(containers).filter(
					(g) =>
						(options.items === null || options.items.includes(g.id)) &&
						(!options.runningOnly || g.members.some((c) => c.state === 'running')),
				)
			: [],
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
	const primaryServices = $derived(sanitizeDockerLayout(session.docker).primaryServices);
	const href = $derived(kind === 'docker' ? '/docker' : kind === 'vms' ? '/vms' : '/shares');
</script>

{#if kind !== 'shares' && !admin}<p class="muted">{t('dashboard.workloadAdmin')}</p>
{:else if error}<p class="notice-warn" role="status">{t('ops.stale')} {error}</p>
{:else if !ready}<p class="muted">{t('common.loading')}</p>
{:else}
	<div class="collection" class:grid={options.list === 'grid'}>
		{#if kind === 'docker'}
			{#each items.slice(0, options.rows) as group (group.id)}
				{@const stack = group.id.startsWith('app:')}
				{@const primary = stack ? stackPrimary(group, primaryServices) : group.members[0]}
				{@const links = primary ? webLinks(primary, host, tailscaleHost) : []}
				{@const running = group.members.filter((c) => c.state === 'running').length}
				<div class="workload" class:stack>
					{#if stack}<button
							class="expand"
							aria-expanded={expanded.includes(group.id)}
							aria-label={group.name}
							onclick={() =>
								(expanded = expanded.includes(group.id)
									? expanded.filter((id) => id !== group.id)
									: [...expanded, group.id])}
							><Icon
								name={expanded.includes(group.id) ? 'chevronDown' : 'arrowRight'}
								size={13}
							/></button
						>{/if}
					<span class="app-icon"><AppIcon name={group.name} src={primary?.icon} /></span>
					<div class="identity">
						<a
							data-edit-part="value"
							href={stack ? `/docker/apps/${encodeURIComponent(group.name)}` : '/docker'}
							>{stack ? group.name : dockerName(group.members[0], 'distinct')}</a
						><span class="state" data-edit-part="labels"
							><i class:ok={running === group.members.length}></i>{stack
								? `${running} / ${group.members.length} ${t('dashboard.servicesRunning')}`
								: tOr(`docker.state.${group.members[0].state}`, group.members[0].state)}</span
						>
					</div>
					{#if options.details}<div class="links">
							{#each links as link}<a
									href={link.href}
									target="_blank"
									rel="noopener noreferrer"
									aria-label={`${group.name} · ${link.label}`}
									title={link.label}><Icon name={link.icon} size={14} /></a
								>{/each}
						</div>{/if}
					{#if stack && expanded.includes(group.id)}<div class="members">
							{#each group.members as c}<div>
									<span>{c.name}</span><span>{tOr(`docker.state.${c.state}`, c.state)}</span>
								</div>{/each}
						</div>{/if}
				</div>
			{:else}<p class="muted">{t('ops.none')}</p>{/each}
		{:else if kind === 'vms'}
			{#each machines.slice(0, options.rows) as vm (vm.name)}<div class="workload">
					<span class="app-icon"><Icon name="monitor" /></span>
					<div class="identity">
						<a data-edit-part="value" href="/vms?vm={encodeURIComponent(vm.name)}">{vmLabel(vm)}</a
						><span class="state" data-edit-part="labels"
							><i class:ok={vm.state === 'running'}></i>{tOr(
								`vms.state.${vm.state}`,
								vm.state,
							)}</span
						>
					</div>
					{#if options.details}<div class="resources" data-edit-part="value">
							<span>{vm.state === 'running' ? formatPercent(vm.cpuPercent) + ' CPU' : '–'}</span
							><span>{vm.state === 'running' ? formatBytes(vm.memoryBytes) + ' RAM' : ''}</span>
						</div>{/if}
				</div>{:else}<p class="muted">{t('ops.none')}</p>{/each}
		{:else}
			{#each folders.slice(0, options.rows) as share (share.name)}<div class="workload">
					<span class="app-icon"><Icon name="folder" /></span>
					<div class="identity">
						<a data-edit-part="value" href="/shares?share={encodeURIComponent(share.name)}"
							>{share.name}</a
						><span class="state" data-edit-part="labels"
							>{share.available ? share.volume : t('storage.notMounted')}</span
						>
					</div>
					{#if options.details}<span class="chip"
							>{[share.smb ? 'SMB' : '', share.nfs?.enabled ? 'NFS' : '']
								.filter(Boolean)
								.join(' · ') || '–'}</span
						>{/if}
				</div>{:else}<p class="muted">{t('shares.empty')}</p>{/each}
		{/if}
	</div>
	<a class="open" {href}>{t('dashboard.openSection')}<Icon name="arrowRight" size={13} /></a>
{/if}

<style>
	.collection {
		display: flex;
		flex-direction: column;
		gap: var(--tile-row-gap, 10px);
	}
	.collection.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(145px, 1fr));
		gap: var(--tile-row-gap, 10px) 14px;
	}
	.workload {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		padding: var(--tile-row-gap, 10px) 0;
	}
	.workload + .workload {
		border-top: 1px solid var(--border);
	}
	.grid .workload {
		border: 0;
	}
	.workload.stack {
		grid-column: 1/-1;
		border-top: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.app-icon {
		width: 30px;
		height: 30px;
		flex: none;
		background: var(--chip);
		border-radius: var(--radius-small);
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--accent);
		font-size: 12px;
	}
	.identity {
		display: flex;
		flex-direction: column;
		gap: 5px;
		min-width: 0;
		flex: 1;
	}
	.identity a {
		color: var(--tile-value-color, var(--text));
		font-size: var(--tile-value-size, 16px);
		font-weight: var(--tile-value-weight, 500);
		font-family: var(--tile-value-font);
		text-align: var(--tile-align);
		overflow-wrap: anywhere;
	}
	.identity a:hover {
		color: var(--accent);
	}
	.state {
		display: flex;
		align-items: center;
		gap: 5px;
		color: var(--tile-label-color, var(--muted));
		font-size: var(--tile-label-size, 12px);
	}
	.state i {
		height: 5px;
		width: 5px;
		border-radius: 50%;
		background: var(--muted);
	}
	.state i.ok {
		background: var(--status-ok);
	}
	.links {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.links a {
		color: var(--muted);
	}
	.expand {
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 2px;
		cursor: pointer;
	}
	.members {
		flex-basis: 100%;
		display: grid;
		gap: 8px;
		margin: 8px 0 0 25px;
		font-size: var(--tile-label-size, 12px);
	}
	.members > div {
		display: flex;
		justify-content: space-between;
		gap: 10px;
		overflow-wrap: anywhere;
	}
	.resources {
		display: grid;
		gap: 5px;
		text-align: right;
		font: var(--tile-label-size, 12px) var(--tile-value-font);
		white-space: nowrap;
	}
	.resources span + span {
		color: var(--muted);
	}
	.open {
		display: flex;
		justify-content: flex-end;
		align-items: center;
		gap: 6px;
		margin-top: 5px;
		font-size: var(--tile-label-size, 12px);
	}
</style>
