<script lang="ts">
	// VMs: the list on the left, the chosen VM on the right with the tabs
	// Overview, Screen and Snapshots.
	import AdoptDialog from './AdoptDialog.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import CloneDialog from './CloneDialog.svelte';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import TasksPanel from '#lib/components/TasksPanel.svelte';
	import type { VM } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { formatBytes } from '#lib/format.ts';
	import { VNC_HEIGHT } from '#lib/vnc-size.ts';
	import { goto } from '$app/navigation';
	import { inWindow } from '#lib/desktop/frame.ts';
	import Overview from './Overview.svelte';
	import ScreenToolbar from './ScreenToolbar.svelte';
	import Snapshots from './Snapshots.svelte';
	import VmActionDialog from './VmActionDialog.svelte';
	import VmButtons from './VmButtons.svelte';
	import VmList from './VmList.svelte';
	import VmRowMenu from './VmRowMenu.svelte';
	import VmXmlDialog from './VmXmlDialog.svelte';
	import VncScreen from './VncScreen.svelte';
	import { noticeClass } from './vm-page.ts';
	import { registerVmsMenu } from './vms-menu.ts';
	import { TABS, VmsPage } from './vms-page.svelte.ts';

	// State and actions live in vms-page.svelte.ts.
	const p = new VmsPage();
	const status = $derived(p.status);
	const current = $derived(p.current);
	registerVmsMenu(p);
</script>

<svelte:document
	onfullscreenchange={() => (p.size.fullscreen = document.fullscreenElement === p.size.frame)}
/>
<!-- The corner belongs to the frame itself; clicks into the picture land on noVNC's canvas. -->
<svelte:window
	onpointerdown={(event) => {
		if (event.target === p.size.frame) p.size.dragStart();
	}}
	onpointerup={p.size.dragEnd}
/>

{#if !status && p.error}
	<div class="notice-error" role="alert">
		<div class="notice-line">
			<span class="notice-text">{p.error.text}</span>
			<button type="button" class="btn small" disabled={p.refreshing} onclick={p.load}
				>{t('common.retry')}</button
			>
		</div>
		{#if p.error.detail}<pre>{p.error.detail}</pre>{/if}
	</div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{/if}
{#if status && p.refreshError}
	<div class="notice-warn" role="alert">
		<div class="notice-line">
			<span class="notice-text"
				>{p.refreshError}
				{t('vms.stale', { time: new Date(p.lastRefresh).toLocaleTimeString() })}</span
			>
			<button type="button" class="btn small" disabled={p.refreshing} onclick={p.load}
				>{t('common.retry')}</button
			>
		</div>
	</div>
{/if}

{#snippet vmState(current: VM)}
	<span class="chip" class:ok={current.state === 'running'}>{t(`vms.state.${current.state}`)}</span>
	{#if current.autostart}<span class="chip">{t('vms.autostart')}</span>{/if}
{/snippet}

{#if status && p.vms.length === 0}
	{#if !status.kvm}<p class="notice-warn" role="status">{t('vms.noKvm')}</p>{/if}
	<PageHead title={t('vms.title')}>
		{#snippet actions()}
			<a class="btn" href="/vms/media">{t('media.title')}</a>
			<button type="button" class="btn" onclick={() => (p.adopting = true)}
				>{t('vms.adopt.open')}</button
			>
			<TasksPanel compact />
			<a class="btn btn-primary" href="/vms/new">{t('vms.create')}</a>
		{/snippet}
	</PageHead>
	<EmptyState title={t('vms.empty')} />
{:else if status && current}
	<div class="vm-page">
		<!-- PLAN 64: in a window the title bar names the VM; state and buttons form the tool bar. -->
		<PageHead title={vmLabel(current)} id="vm-title">
			{#snippet actions()}
				{#if !p.phone}<strong class="bar-name" aria-hidden="true">{vmLabel(current)}</strong
					>{@render vmState(current)}<span class="spacer"></span>{/if}
				<VmButtons {p} {current} tasks={false} />
			{/snippet}
		</PageHead>
		<!-- On the phone the bar keeps only the buttons; the state gets a row of its own. -->
		{#if p.phone}<div class="phone-state">
				{@render vmState(current)}<TasksPanel compact />
			</div>{/if}
		{#if current.description || current.ioError}<div class="title">
				{#if current.description}<span class="description muted">{current.description}</span>{/if}
				{#if current.ioError}<p class="notice-error io-error" role="alert">
						{t('vms.ioError')}
						<button type="button" class="btn small" onclick={() => p.act(current, 'resume')}
							>{t('vms.resume')}</button
						>
					</p>{/if}
			</div>{/if}

		{#if p.notices.length}
			<div class="notices">
				{#each p.allNotices ? p.notices : p.notices.slice(0, 1) as notice, index (index)}
					<div
						class={noticeClass[notice.level]}
						role={notice.level === 'error' ? 'alert' : 'status'}
					>
						<div class="notice-line">
							<span class="notice-text">{notice.text}</span>
							{#if notice.retry && p.failures[current.name]?.action}<button
									type="button"
									class="btn small"
									disabled={p.isBusy(current)}
									onclick={() => p.act(current, p.failures[current.name].action)}
									>{t('ui.retry')}</button
								>{/if}
							{#if index === 0 && p.notices.length > 1}
								<button
									type="button"
									class="more-notices"
									aria-expanded={p.allNotices}
									onclick={() => (p.allNotices = !p.allNotices)}
									>{p.allNotices
										? t('vms.noticesLess')
										: t('vms.noticesMore', { count: p.notices.length - 1 })}</button
								>
							{/if}
						</div>
						{#if notice.detail}<pre>{notice.detail}</pre>{/if}
					</div>
				{/each}
			</div>
		{/if}

		<div class="command-bar">
			<nav class="tabs" aria-label={t('vms.tabs')}>
				{#each TABS as entry (entry)}
					<a
						href={p.link(current, entry)}
						aria-current={p.tab === entry ? 'page' : undefined}
						data-sveltekit-replacestate
						data-sveltekit-noscroll>{t(`vms.tab.${entry}`)}</a
					>
				{/each}
			</nav>

			{#if p.tab === 'screen'}
				<ScreenToolbar {p} {current} />
			{/if}
		</div>
		<div class="layout">
			{#if !inWindow && (p.sidebar || p.tab !== 'screen')}
				<VmList {p} {current} />
			{/if}

			<section class="main" aria-labelledby="vm-title">
				{#if p.tab === 'overview' || p.tab === 'hardware' || p.tab === 'schedule'}
					<Overview
						bind:this={p.overview}
						part={p.tab}
						vm={current}
						history={p.history[current.name] ?? []}
						volumes={storage.status?.volumes ?? []}
						canStart={p.canStart(current)}
						busy={p.isBusy(current)}
						onstart={() => p.act(current, 'start')}
						onchanged={p.load}
						onsnapshots={() => goto(p.link(current, 'snapshots'), { replaceState: true })}
					/>
					<!-- Removing stands apart, at the end of the hardware. -->
					{#if p.tab === 'hardware'}<DangerZone
							tile
							hint={t(current.state === 'running' ? 'vms.removeStopFirst' : 'vms.removeHint')}
						>
							<button
								type="button"
								class="btn btn-danger"
								disabled={p.isBusy(current) || current.state === 'running'}
								onclick={() => p.open('remove')}>{t('vms.remove')}</button
							>
						</DangerZone>{/if}
				{:else if p.tab === 'snapshots'}
					<Snapshots vm={current} />
				{:else}
					{#if p.size.error}<p class="notice-error" role="alert">
							{t('vms.size.failed')}
							{p.size.error}
						</p>{/if}
					<div
						class="frame"
						style:height={p.size.fullscreen ? null : `${p.size.fitted}px`}
						style:width={p.size.fullscreen || p.size.width === null ? null : `${p.size.width}px`}
						style:max-height={p.size.fullscreen || p.size.room <= 0
							? null
							: `${Math.max(VNC_HEIGHT.min, Math.floor(p.size.room))}px`}
						class:fullscreen={p.size.fullscreen}
						bind:this={p.size.frame}
					>
						{#if current.state === 'running'}
							{#key current.name}
								<VncScreen
									bind:this={p.screen}
									vm={current.name}
									mode={p.mode}
									resizable={current.template === 'linux'}
									onstatus={(phase) => (p.connectionPhase = phase)}
								/>
							{/key}
						{/if}
					</div>
					<div class="screen-footer">
						{#if current.state !== 'running'}
							<div class="screen-state">
								<span class="muted">{t('vms.off', { name: vmLabel(current) })}</span><button
									type="button"
									class="btn btn-primary"
									onclick={() => p.act(current, 'start')}
									disabled={p.isBusy(current) || !p.canStart(current)}>{t('vms.startVm')}</button
								>
							</div>
						{:else if p.connectionPhase !== 'connected'}
							<p class="muted hint" role="status">
								{t(
									p.connectionPhase === 'disabled'
										? 'vms.screenDisabled'
										: p.connectionPhase === 'lost'
											? 'vms.screenLost'
											: 'vms.screenConnecting',
								)}
							</p>
						{/if}
						<p class="muted hint">{t('vms.hint')}</p>
					</div>
				{/if}
			</section>
		</div>
	</div>
{/if}

{#if status && p.vms.length > 0}
	{@const memory = p.vms
		.filter((vm) => vm.state === 'running')
		.reduce((sum, vm) => sum + vm.memoryMiB, 0)}
	<!-- PLAN 75-19, style W4: the figures in one line at the bottom. -->
	<StatusBar label={t('vms.title')}>
		<span>{t('vms.title')} <b>{p.counts.all}</b></span>
		<span>{t('vms.filter.running')} <b>{p.counts.running}</b></span>
		<span>{t('vms.memoryRunning')} <b>{formatBytes(memory * 1024 * 1024)}</b></span>
	</StatusBar>
{/if}
{#if p.rowMenu}
	{@const vm = p.rowMenu.vm}
	<VmRowMenu
		{vm}
		x={p.rowMenu.x}
		y={p.rowMenu.y}
		busy={p.isBusy(vm)}
		canStart={p.canStart(vm)}
		onclose={() => (p.rowMenu = null)}
		onchoose={(choice) => p.rowAction((vm) => p.rowChoice(vm, choice))}
	/>
{/if}

{#if p.dialog?.kind === 'xml'}
	<VmXmlDialog text={p.dialog.text} onclose={() => (p.dialog = null)} />
{:else if p.dialog && p.dialogVM}
	<VmActionDialog
		kind={p.dialog.kind}
		vm={p.dialogVM}
		onclose={() => (p.dialog = null)}
		onsubmit={p.submit}
	/>
{/if}

{#if p.adopting}<AdoptDialog
		onclose={() => (p.adopting = false)}
		onadopted={async (name) => {
			p.adopting = false;
			await p.load();
			goto(`/vms?vm=${encodeURIComponent(name)}`);
		}}
	/>{/if}

{#if p.cloning}<CloneDialog
		source={p.cloning}
		onclose={() => (p.cloning = null)}
		onstarted={() => {
			p.cloning = null;
			p.load();
		}}
	/>{/if}

<style>
	/* PLAN 67: the VM's name leads the tool bar (the heading itself stays for screen readers). */
	.bar-name {
		min-width: 0;
		max-width: 40%;
		font-size: var(--text-lg);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.vm-page {
		width: 100%;
		max-width: 1480px;
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.command-bar {
		display: flex;
		align-items: center;
		gap: var(--space-5);
		min-height: 48px;
		border-bottom: 1px solid var(--border);
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-5);
	}
	.main {
		flex: 999 1 560px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.description {
		font-size: var(--text-md);
	}
	.icon-btn {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.notices {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.notice-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5) var(--space-3);
	}
	.notice-text {
		flex: 1 1 16rem;
	}
	.more-notices {
		padding: 0;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		font-size: var(--text-base);
		text-decoration: underline;
		cursor: pointer;
	}
	.btn.small {
		height: 2rem;
		padding: 0 var(--space-2-5);
		font-size: var(--text-base);
	}
	.tabs {
		display: flex;
		gap: 26px;
		align-self: stretch;
		align-items: center;
		flex: none;
	}
	.tabs a {
		display: flex;
		align-items: center;
		align-self: stretch;
		padding: 0 2px;
		border-bottom: 2px solid transparent;
		color: var(--muted);
		font-size: var(--text-md);
		text-decoration: none;
		white-space: nowrap;
	}
	.tabs a:hover {
		color: var(--text);
	}
	.tabs a[aria-current='page'] {
		border-bottom-color: var(--accent);
		color: var(--text);
		font-weight: 600;
	}
	/* Width and height are dragged at the bottom right corner; it is never
	   wider than its column and never taller than the rest of the window
	   (max-height is set from the page). */
	.frame {
		position: relative;
		width: 100%;
		max-width: 100%;
		min-width: min(320px, 100%);
		min-height: 240px;
		background: var(--screen-bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		overflow: hidden;
		resize: both;
	}
	.frame.fullscreen {
		width: 100%;
		height: 100%;
		max-height: none;
		border: 0;
		border-radius: 0;
		resize: none;
	}
	.hint {
		font-size: var(--text-sm);
	}
	.screen-footer {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.screen-state {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	@media (max-width: 1100px) {
		.command-bar {
			flex-wrap: wrap;
			gap: var(--space-2);
		}
		.tabs {
			min-height: 44px;
			/* Five tabs (PLAN 67): they scroll sideways in a narrow window, the page does not. */
			flex: 1 1 100%;
			min-width: 0;
			gap: 20px;
			overflow-x: auto;
			scrollbar-width: none;
		}
	}
	/* PLAN 64: the state at the left of the window's tool bar, buttons right. */
	.spacer {
		flex: 1;
	}
	.phone-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
</style>
