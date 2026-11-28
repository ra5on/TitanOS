<script lang="ts">
	import type { DockerPage } from './docker-page.svelte.ts';
	import type { DockerGroup } from '#lib/docker-layout.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { isRunning, portLabels } from './container.ts';
	let {
		p,
		activeGroup: group,
		area,
	}: { p: DockerPage; activeGroup?: DockerGroup; area: string } = $props();
	let searching = $state(false);
	const sections = [
		['containers', 'docker.detail.containers'],
		['resources', 'nord.docker.resources'],
		['manage', 'nord.docker.manage'],
	] as const;
	const resources = [
		['/docker/images', 'box', 'dres.images.title'],
		['/docker/volumes', 'disk', 'dres.volumes.title'],
		['/docker/networks', 'globe', 'dnet.title'],
		['/docker/cleanup', 'trash', 'dres.cleanup.title'],
	] as const;
</script>

{#if group}
	<div class="nord-summary">
		<div>
			<small>{t('docker.table.colState')}</small><strong
				>{t(`docker.table.state.${p.rowState(group)}`)}</strong
			>
		</div>
		<div><small>{t('docker.cpu')}</small><strong>{formatPercent(p.groupCpu(group))}</strong></div>
		<div><small>{t('docker.ram')}</small><strong>{formatBytes(p.groupRam(group))}</strong></div>
	</div>
	<div class="tools">
		<button
			class="btn"
			disabled={p.globalBusy || p.groupBusy(group)}
			onclick={() => group && p.groupAction(group, p.isOn(group) ? 'stop' : 'start')}
			>{t(p.isOn(group) ? 'docker.stop' : 'docker.start')}</button
		>
		<button
			class="btn"
			disabled={p.globalBusy || p.groupBusy(group) || !p.isOn(group)}
			onclick={() => group && p.groupAction(group, 'restart')}>{t('docker.restart')}</button
		>
		<button
			class="btn"
			aria-label={t('docker.actionsFor', { name: group.name })}
			onclick={(e) => group && p.openRowMenu(group, e.currentTarget)}
			><Icon name="more" size={18} /></button
		>
	</div>
	{#if p.groupApp(group)}<SettingRow
			icon="edit"
			title={t('docker.edit')}
			href={'/docker/apps/' + encodeURIComponent(p.groupApp(group))}
		/>{/if}
	<div class="nord-list">
		{#each group.members as c (c.id)}
			<details class="service">
				<summary
					><span>{c.service || c.name}</span><small
						>{t(`docker.table.state.${p.memberState(c)}`)}</small
					></summary
				>
				<div class="service-content">
					<strong>{c.image}</strong>
					{#if isRunning(c)}<span>{formatPercent(c.cpuPercent)} · {formatBytes(c.memoryBytes)}</span
						>{/if}
					{#if portLabels(c).length}<span
							>{t('docker.column.ports')} · {portLabels(c).join(', ')}</span
						>{/if}
					<button class="btn" onclick={() => (p.dialog = { kind: 'logs', container: c })}
						>{t('docker.logs')}</button
					>
				</div>
			</details>
		{/each}
	</div>
{:else}
	<div class="nord-summary" aria-label={t('docker.title')}>
		<div>
			<small><Icon name="box" size={14} />{t('docker.detail.containers')}</small><strong
				>{p.runCount} <small>/ {p.containers.length}</small></strong
			>
		</div>
		<div>
			<small><Icon name="chip" size={14} />{t('docker.cpu')}</small><strong
				>{formatPercent(p.totalCpu)}</strong
			>
		</div>
		<div>
			<small><Icon name="memory" size={14} />{t('docker.ram')}</small><strong
				>{formatBytes(p.totalRam)}</strong
			>
		</div>
	</div>
	<nav class="nord-segments" aria-label={t('docker.title')}>
		{#each sections as [id, key] (id)}<a
				href={id === 'containers' ? '/docker' : '/docker?area=' + id}
				aria-current={area === id || (id === 'containers' && !sections.some(([s]) => s === area))
					? 'page'
					: undefined}>{t(key)}</a
			>{/each}
	</nav>
	{#if area === 'resources'}
		<div class="nord-list">
			{#each resources as [href, icon, key] (href)}<SettingRow
					{href}
					{icon}
					title={t(key)}
				/>{/each}
		</div>
	{:else if area === 'manage'}
		<div class="nord-list management">
			<button onclick={() => p.open('settings')}
				><Icon name="gear" />{t('docker.settingsTitle')}</button
			>
			<button onclick={() => (p.autoUpdates = true)}
				><Icon name="refresh" />{t('autoUpdate.button')}</button
			>
			<button disabled={p.globalBusy} onclick={p.checkUpdates}
				><Icon name="download" />{t('docker.checkUpdates')}</button
			>
			<button disabled={p.globalBusy || !p.updatable} onclick={p.askUpdates}
				><Icon name="download" />{t('docker.updateAllCount', {
					count: p.updateApps.length,
				})}</button
			>
			<button disabled={p.globalBusy || p.runCount >= p.containers.length} onclick={p.startAll}
				><Icon name="play" />{t('docker.startAll')}</button
			>
			<button disabled={p.globalBusy || !p.runCount} onclick={() => (p.confirmStopAll = true)}
				><Icon name="stop" />{t('docker.stopAll')}</button
			>
			<button onclick={() => p.open('import')}><Icon name="upload" />{t('apps.import')}</button>
			<SettingRow href="/apps/new" icon="plus" title={t('docker.ownContainer')} />
		</div>
	{:else}
		<div class="section-head">
			<h2>{t('docker.table.title')}</h2>
			<button
				class="btn"
				aria-label={t('docker.search')}
				aria-expanded={searching}
				onclick={() => {
					searching = !searching;
					if (!searching) p.search = '';
				}}><Icon name="search" size={18} /></button
			>
		</div>
		<!-- §121: the filters as buttons in a row of their own, not a system list. -->
		<div class="chips" role="group" aria-label={t('docker.filter')}>
			{#each p.FILTERS as [id, label] (id)}
				<button
					type="button"
					class="chip-btn"
					aria-pressed={p.filter === id}
					onclick={() => (p.filter = id)}>{t(label)} {p.counts[id]}</button
				>
			{/each}
		</div>
		{#if searching}<input
				type="search"
				class="field"
				placeholder={t('docker.search')}
				aria-label={t('docker.search')}
				bind:value={p.search}
			/>{/if}
		<div class="nord-list">
			{#each p.shownGroups as g (g.id)}
				<a
					class="container-row"
					data-app={g.name}
					href={'/docker?group=' + encodeURIComponent(g.id)}
				>
					<span class="app-art"
						><AppIcon src={g.members.find((c) => c.icon)?.icon || ''} name={g.name} /></span
					>
					<span class="label"><b>{g.name}</b><small>{p.subLine(g)}</small></span>
					<span class="state" class:on={p.isOn(g)}
						>{t(`docker.table.state.${p.rowState(g)}`)}{#if p.groupUpdate(g)}
							· {t('docker.update')}{/if}</span
					>
					<Icon name="chevronRight" size={15} />
				</a>
			{:else}<p>{t(p.groups.length ? 'docker.noMatch' : 'docker.empty')}</p>{/each}
		</div>
		<a class="back" href="/apps"><Icon name="plus" size={16} />{t('docker.installApp')}</a>
	{/if}
{/if}

<style>
	.tools,
	.section-head,
	.back,
	.container-row,
	.service-content,
	.management button {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.tools .btn {
		min-height: 44px;
		flex: 1;
	}
	.section-head h2 {
		flex: 1;
		font-size: var(--text-md);
		margin: 0;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.chip-btn {
		min-height: 40px;
		padding: 0 14px;
		border-radius: 999px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
	.chip-btn[aria-pressed='true'] {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.section-head .btn {
		width: 44px;
		padding: 0;
	}
	.back {
		min-height: 44px;
		text-decoration: none;
		color: var(--text);
		font-weight: 600;
		align-self: flex-start;
	}
	.container-row,
	.management button {
		min-height: 56px;
		width: 100%;
		padding: 9px 12px;
		color: var(--text);
		text-decoration: none;
		font: inherit;
		background: none;
		border: none;
		text-align: left;
	}
	.management button {
		cursor: pointer;
	}
	.container-row + .container-row,
	.management > * + *,
	.service + .service {
		border-top: 1px solid var(--nord-line);
	}
	.app-art {
		display: grid;
		place-items: center;
		width: 38px;
		height: 38px;
		background: var(--chip);
		border-radius: 10px;
		overflow: hidden;
		flex: none;
	}
	.label {
		display: grid;
		flex: 1;
		min-width: 0;
	}
	.label > * {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	small,
	.state {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.state {
		max-width: 27%;
		overflow-wrap: anywhere;
	}
	.state.on::before {
		display: inline-block;
		content: '';
		border-radius: 100%;
		width: 6px;
		height: 6px;
		background: var(--ok);
		margin-inline-end: 4px;
	}
	.service summary {
		cursor: pointer;
		padding: 12px;
		min-height: 48px;
		overflow-wrap: anywhere;
	}
	.service summary small {
		margin-inline-start: 8px;
	}
	.service-content {
		align-items: flex-start;
		flex-direction: column;
		padding: 0 12px 12px;
		overflow-wrap: anywhere;
	}
	p {
		margin: 12px;
	}
</style>
