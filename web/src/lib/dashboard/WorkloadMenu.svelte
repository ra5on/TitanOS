<script lang="ts">
	import { api, ApiError, type DockerContainer, type VM } from '#lib/api.ts';
	import { t, errorText } from '#lib/i18n/index.svelte.ts';
	import { goto } from '$app/navigation';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { webLinks, type WebLink } from '../../routes/docker/container.ts';
	import VMDetails from './VMDetails.svelte';
	import type { TileId } from './tiles.ts';
	export type WorkloadTarget =
		| {
				kind: 'docker';
				name: string;
				members: DockerContainer[];
				primary?: DockerContainer;
				stack: boolean;
		  }
		| { kind: 'vm'; vm: VM };
	let {
		target,
		x,
		y,
		host,
		tailscaleHost,
		onclose,
		onrefresh,
		onpin,
		oncontainers,
	}: {
		target: WorkloadTarget;
		x: number;
		y: number;
		host: string;
		tailscaleHost: string;
		onclose: () => void;
		onrefresh: () => void;
		onpin: (id: TileId) => void;
		oncontainers: () => void;
	} = $props();
	let menuOpen = $state(true),
		busy = $state(false),
		error = $state('');
	let dialog = $state<'' | 'logs' | 'remove' | 'details' | 'confirm'>('');
	let logs = $state(''),
		confirm = $state(''),
		pending = $state('');
	const name = $derived(target.kind === 'vm' ? vmLabel(target.vm) : target.name);
	const running = $derived(
		target.kind === 'vm'
			? target.vm.state === 'running'
			: target.members.some((c) => ['running', 'restarting', 'paused'].includes(c.state)),
	);
	const links = $derived(
		target.kind === 'docker' && target.primary ? webLinks(target.primary, host, tailscaleHost) : [],
	);
	function close() {
		if (!busy) onclose();
	}
	function pick(fn: () => void) {
		menuOpen = false;
		fn();
	}
	async function run(action: string) {
		menuOpen = false;
		busy = true;
		error = '';
		try {
			if (target.kind === 'vm')
				await api.post(`/vms/${encodeURIComponent(target.vm.name)}/${action}`);
			else if (action === 'check') await api.post('/docker/updates/check');
			else
				for (const c of target.members.filter((c) =>
					action === 'stop'
						? ['running', 'restarting', 'paused'].includes(c.state)
						: action === 'start'
							? !['running', 'restarting'].includes(c.state)
							: true,
				))
					await api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`);
			onrefresh();
			onclose();
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
			dialog = 'confirm';
			pending = action;
		} finally {
			busy = false;
			onrefresh();
		}
	}
	function ask(action: string) {
		pending = action;
		dialog = 'confirm';
		error = '';
		menuOpen = false;
	}
	async function showLogs(c: DockerContainer) {
		pick(() => (dialog = 'logs'));
		logs = '';
		error = '';
		try {
			logs = (
				await api.get<{ logs: string }>(
					`/docker/containers/${encodeURIComponent(c.id)}/logs?tail=300`,
				)
			).logs;
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
		}
	}
	async function remove() {
		busy = true;
		error = '';
		try {
			if (target.kind === 'vm')
				await api.delete(`/vms/${encodeURIComponent(target.vm.name)}`, {
					deleteDisks: false,
					confirm,
				});
			else if (target.stack || target.members[0].managed)
				await api.delete(
					`/docker/apps/${encodeURIComponent(target.members[0].app || target.name)}`,
				);
			else await api.delete(`/docker/containers/${encodeURIComponent(target.members[0].id)}`);
			onrefresh();
			onclose();
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
		} finally {
			busy = false;
		}
	}
	function pin() {
		pick(() => {
			onpin(
				target.kind === 'vm'
					? `vm:${target.vm.name}`
					: target.stack
						? `app:${target.name}`
						: `container:${target.members[0].name}`,
			);
			onclose();
		});
	}
</script>

{#if menuOpen}<ContextMenu label={t('dashboard.menu', { name })} {x} {y} onclose={close}>
		<div class="menu-title">{name}</div>
		{#each links as link}<a
				role="menuitem"
				href={link.href}
				target="_blank"
				rel="noopener noreferrer"
				onclick={onclose}><Icon name={link.icon} size={16} /><span>{link.label}</span></a
			>{/each}
		{#if target.kind === 'docker'}
			{#if target.stack}<button
					role="menuitem"
					onclick={() =>
						pick(() => {
							oncontainers();
							onclose();
						})}
					><Icon name="list" size={16} /><span
						>{t('dashboard.containers')} ({target.members.length})</span
					></button
				>{/if}
			{#if target.primary?.state === 'running'}<button
					role="menuitem"
					onclick={() =>
						pick(() => {
							if (target.kind === 'docker' && target.primary)
								addContainerConsole(target.primary.id, dockerName(target.primary, 'distinct'));
							onclose();
						})}><Icon name="terminal" size={16} /><span>{t('docker.console')}</span></button
				>{/if}
			{#each target.members as c}<button role="menuitem" onclick={() => showLogs(c)}
					><Icon name="list" size={16} /><span
						>{t('docker.logs')}{target.stack ? ` · ${c.service || c.name}` : ''}</span
					></button
				>{/each}
			<a
				role="menuitem"
				href={target.stack
					? `/docker/apps/${encodeURIComponent(target.members[0].app || target.name)}`
					: '/docker'}><Icon name="list" size={16} /><span>{t('dashboard.showDetails')}</span></a
			>
		{:else}
			{#if running}<a role="menuitem" href="/vms?vm={encodeURIComponent(target.vm.name)}&tab=screen"
					><Icon name="monitor" size={16} /><span>{t('vms.tab.screen')}</span></a
				>{/if}
			<button role="menuitem" onclick={() => pick(() => (dialog = 'details'))}
				><Icon name="list" size={16} /><span>{t('dashboard.showDetails')}</span></button
			>
		{/if}
		<hr />
		{#if running}<button
				role="menuitem"
				onclick={() => (target.kind === 'vm' ? ask('reboot') : run('restart'))}
				><Icon name="restart" size={16} /><span
					>{t(target.kind === 'vm' ? 'vms.reboot' : 'docker.restart')}</span
				></button
			><button
				role="menuitem"
				onclick={() => (target.kind === 'vm' ? ask('shutdown') : run('stop'))}
				><Icon name="stop" size={16} /><span
					>{t(target.kind === 'vm' ? 'vms.shutdown' : 'docker.stop')}</span
				></button
			>{:else}<button
				role="menuitem"
				disabled={target.kind === 'vm' && (!target.vm.available || !!target.vm.job)}
				onclick={() => run('start')}
				><Icon name="play" size={16} /><span
					>{t(target.kind === 'vm' ? 'vms.start' : 'docker.start')}</span
				></button
			>{/if}
		<hr />
		{#if target.kind === 'docker'}<button role="menuitem" onclick={() => run('check')}
				><Icon name="restart" size={16} /><span>{t('docker.checkUpdates')}</span></button
			>{#if target.stack || target.members[0].managed}<a
					role="menuitem"
					href="/docker/apps/{encodeURIComponent(target.members[0].app)}"
					><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
				>{/if}
		{:else}<a role="menuitem" href="/vms/edit?vm={encodeURIComponent(target.vm.name)}"
				><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></a
			><a role="menuitem" href="/vms?vm={encodeURIComponent(target.vm.name)}&tab=snapshots"
				><Icon name="camera" size={16} /><span>{t('snapshots.title')}</span></a
			>{/if}
		<button role="menuitem" onclick={pin}
			><Icon name="plus" size={16} /><span>{t('dashboard.pin')}</span></button
		>
		{#if target.kind === 'docker' || !running}<hr />
			<button role="menuitem" class="danger" onclick={() => pick(() => (dialog = 'remove'))}
				><Icon name="trash" size={16} /><span
					>{t(target.kind === 'docker' ? 'docker.remove' : 'vms.remove')}</span
				></button
			>{/if}
	</ContextMenu>{/if}
{#if busy && !dialog}<Dialog title={name} locked onclose={close}
		><p>{t('common.loading')}</p></Dialog
	>{/if}
{#if dialog === 'details' && target.kind === 'vm'}<VMDetails vms={[target.vm]} onclose={close} />
{:else if dialog}<Dialog
		title={dialog === 'logs' ? t('docker.logsTitle', { name }) : name}
		wide={dialog === 'logs'}
		locked={busy}
		onclose={close}
	>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if dialog === 'logs'}<pre>{logs || (!error ? t('docker.logsEmpty') : '')}</pre>
		{:else if dialog === 'remove'}<p>
				{t(target.kind === 'vm' ? 'dashboard.removeVm' : 'dashboard.removeDocker', { name })}
			</p>
			{#if target.kind === 'vm'}<label
					>{t('dashboard.confirmName', { name: target.vm.name })}<input
						class="field"
						bind:value={confirm}
						autocomplete="off"
					/></label
				>{/if}
		{:else}<p>
				{t('dashboard.confirmVmAction', {
					name,
					action: t(
						pending === 'shutdown'
							? 'vms.shutdown'
							: pending === 'reboot'
								? 'vms.reboot'
								: 'ui.retry',
					),
				})}
			</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={close}
				>{t(dialog === 'logs' ? 'common.done' : 'common.cancel')}</button
			>{#if dialog === 'remove'}<button
					class="btn btn-danger"
					disabled={busy || (target.kind === 'vm' && confirm !== target.vm.name)}
					onclick={remove}>{t(target.kind === 'vm' ? 'vms.remove' : 'docker.removeSubmit')}</button
				>{:else if dialog === 'confirm'}<button
					class="btn btn-warn"
					disabled={busy}
					onclick={() => run(pending)}>{t('common.done')}</button
				>{/if}
		</div>
	</Dialog>{/if}

<style>
	pre {
		font:
			0.8rem/1.6 'IBM Plex Mono',
			monospace;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 65vh;
		overflow: auto;
	}
	label {
		display: grid;
		gap: 8px;
		margin-top: 14px;
		font-size: 0.85rem;
	}
</style>
