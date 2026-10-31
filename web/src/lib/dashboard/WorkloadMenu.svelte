<script lang="ts">
	import { api, type DockerContainer, type DockerStatus, type VM } from '#lib/api.ts';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { webLinks } from '../../routes/docker/container.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
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
		kvm = false,
		ondetails,
	}: {
		target: WorkloadTarget;
		x: number;
		y: number;
		host: string;
		tailscaleHost: string;
		onclose: () => void;
		onrefresh: () => void;
		onpin?: (id: TileId) => void;
		kvm?: boolean;
		ondetails: (name: string) => void;
		oncontainers: () => void;
	} = $props();
	let menuOpen = $state(true),
		busy = $state(false),
		error = $state('');
	let dialog = $state<'' | 'logs' | 'remove' | 'checked' | 'confirm'>('');
	let checkResult = $state('');
	let logs = $state(''),
		pending = $state('');
	const name = $derived(target.kind === 'vm' ? vmLabel(target.vm) : target.name);
	const running = $derived(
		target.kind === 'vm'
			? target.vm.state === 'running'
			: target.members.some((c) => ['running', 'restarting', 'paused'].includes(c.state)),
	);
	const removeApp = $derived(
		target.kind === 'docker' && target.members.some((c) => c.managed)
			? target.members.find((c) => c.managed)?.app || (target.stack ? target.name : '')
			: '',
	);
	const removeName = $derived(removeApp || name);
	const managed = $derived(target.kind === 'docker' && target.members.some((c) => c.managed));
	const links = $derived(
		target.kind === 'docker' && target.primary ? webLinks(target.primary, host, tailscaleHost) : [],
	);
	// "nas.local:8096" beside an address, like on the Docker page.
	function where(href: string): string {
		try {
			return new URL(href).host;
		} catch {
			return '';
		}
	}
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
			else if (action === 'check') {
				const result = await api.post<DockerStatus>('/docker/updates/check');
				const selected = result.containers.filter(
					(c) => target.kind === 'docker' && target.members.some((m) => m.name === c.name),
				);
				checkResult =
					selected
						.map(
							(c) =>
								`${dockerName(c, 'distinct')}: ${tOr(`dashboard.update.${c.updateState}`, c.updateState || t('dashboard.updateUnknown'))}`,
						)
						.join('\n') || t('dashboard.updateUnknown');
				dialog = 'checked';
				return;
			} else
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
			error = apiErrorText(e);
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
		logs = t('common.loading');
		error = '';
		try {
			logs = (
				await api.get<{ logs: string }>(
					`/docker/containers/${encodeURIComponent(c.id)}/logs?tail=300`,
				)
			).logs;
		} catch (e) {
			logs = '';
			error = apiErrorText(e);
		}
	}
	async function remove() {
		busy = true;
		error = '';
		try {
			if (target.kind === 'vm')
				await api.delete(`/vms/${encodeURIComponent(target.vm.name)}`, { deleteDisks: false });
			else if (removeApp) await api.delete(`/docker/apps/${encodeURIComponent(removeApp)}`);
			else await api.delete(`/docker/containers/${encodeURIComponent(target.members[0].id)}`);
			onrefresh();
			onclose();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	function pin() {
		pick(() => {
			onpin?.(
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
		<div class="menu-title">
			{#if target.kind === 'docker'}<span class="menu-app-icon"
					><AppIcon
						{name}
						src={target.primary?.icon || target.members.find((c) => c.icon)?.icon}
					/></span
				>{/if}{name}
		</div>
		{#if target.kind === 'docker'}
			<!-- The same order as the menu on the Docker page: Start/Stop,
			     addresses, then the actions, removing last. -->
			{#if running}<button role="menuitem" onclick={() => run('stop')}
					><Icon name="stop" size={16} /><span>{t('docker.stop')}</span></button
				>{:else}<button role="menuitem" onclick={() => run('start')}
					><Icon name="play" size={16} /><span>{t('docker.start')}</span></button
				>{/if}
			<hr />
			{#if links.length > 0}
				{#each links as link}<a
						role="menuitem"
						href={link.href}
						target="_blank"
						rel="noopener noreferrer"
						title={link.label}
						onclick={onclose}
						><Icon name={link.icon} size={16} /><span>{link.text}</span><small class="where"
							>{where(link.href)}</small
						></a
					>{/each}
				<hr />
			{/if}
			{#each target.members as c}<button role="menuitem" onclick={() => showLogs(c)}
					><Icon name="fileText" size={16} /><span
						>{t('docker.logs')}{target.stack ? ` · ${c.service || c.name}` : ''}</span
					></button
				>{/each}
			{#if target.primary?.state === 'running'}<button
					role="menuitem"
					onclick={() =>
						pick(() => {
							if (target.kind === 'docker' && target.primary)
								addContainerConsole(target.primary.id, dockerName(target.primary, 'distinct'));
							onclose();
						})}><Icon name="terminal" size={16} /><span>{t('docker.console')}</span></button
				>{/if}
			{#if managed}<a role="menuitem" href="/docker/apps/{encodeURIComponent(removeApp)}"
					><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
				>{:else}<a role="menuitem" href="/docker"
					><Icon name="list" size={16} /><span>{t('dashboard.showDetails')}</span></a
				>{/if}
			{#if running}<button role="menuitem" onclick={() => run('restart')}
					><Icon name="restart" size={16} /><span>{t('docker.restart')}</span></button
				>{/if}
			<button role="menuitem" onclick={() => run('check')}
				><Icon name="download" size={16} /><span>{t('docker.checkUpdates')}</span></button
			>
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
			{#if onpin}<button role="menuitem" onclick={pin}
					><Icon name="plus" size={16} /><span>{t('dashboard.pin')}</span></button
				>{/if}
			{#if !target.stack || managed}<hr />
				<button role="menuitem" class="danger" onclick={() => pick(() => (dialog = 'remove'))}
					><Icon name="trash" size={16} /><span
						>{removeApp
							? t('dashboard.removeAppAction', { name: removeApp })
							: t('docker.remove')}</span
					></button
				>{/if}
		{:else}
			{#if running}<a role="menuitem" href="/vms?vm={encodeURIComponent(target.vm.name)}&tab=screen"
					><Icon name="monitor" size={16} /><span>{t('vms.tab.screen')}</span></a
				>{/if}
			<button
				role="menuitem"
				onclick={() =>
					pick(() => {
						if (target.kind === 'vm') ondetails(target.vm.name);
						onclose();
					})}><Icon name="list" size={16} /><span>{t('dashboard.showDetails')}</span></button
			>
			<hr />
			{#if running}<button role="menuitem" onclick={() => ask('reboot')}
					><Icon name="restart" size={16} /><span>{t('vms.reboot')}</span></button
				><button role="menuitem" onclick={() => ask('shutdown')}
					><Icon name="stop" size={16} /><span>{t('vms.shutdown')}</span></button
				>{:else}<button
					role="menuitem"
					disabled={!kvm || !target.vm.available || !!target.vm.job}
					title={!kvm ? t('vms.noKvm') : undefined}
					onclick={() => run('start')}
					><Icon name="play" size={16} /><span>{t('vms.start')}</span></button
				>{/if}
			<hr />
			<a role="menuitem" href="/vms/edit?vm={encodeURIComponent(target.vm.name)}"
				><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></a
			><a role="menuitem" href="/vms?vm={encodeURIComponent(target.vm.name)}&tab=snapshots"
				><Icon name="camera" size={16} /><span>{t('snapshots.title')}</span></a
			>
			{#if onpin}<button role="menuitem" onclick={pin}
					><Icon name="plus" size={16} /><span>{t('dashboard.pin')}</span></button
				>{/if}
			{#if !running}<hr />
				<button role="menuitem" class="danger" onclick={() => pick(() => (dialog = 'remove'))}
					><Icon name="trash" size={16} /><span>{t('vms.remove')}</span></button
				>{/if}
		{/if}
	</ContextMenu>{/if}
{#if busy && !dialog}<Dialog title={name} locked onclose={close}
		><p>{t('common.loading')}</p></Dialog
	>{/if}
{#if dialog}<Dialog
		title={dialog === 'logs'
			? t('docker.logsTitle', { name })
			: dialog === 'remove'
				? removeName
				: name}
		wide={dialog === 'logs'}
		locked={busy}
		onclose={close}
	>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if dialog === 'logs'}<pre>{logs || (!error ? t('docker.logsEmpty') : '')}</pre>
		{:else if dialog === 'checked'}<pre>{checkResult}</pre>
		{:else if dialog === 'remove'}<p>
				{removeApp
					? t('dashboard.removeManagedApp', { name: removeApp })
					: t(target.kind === 'vm' ? 'dashboard.removeVm' : 'dashboard.removeDocker', { name })}
			</p>
		{:else}<p>
				{t(target.kind === 'vm' ? 'dashboard.confirmVmAction' : 'dashboard.retryDockerAction', {
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
				>{t(dialog === 'logs' || dialog === 'checked' ? 'common.done' : 'common.cancel')}</button
			>{#if dialog === 'remove'}<button class="btn btn-danger" disabled={busy} onclick={remove}
					>{t(target.kind === 'vm' ? 'vms.remove' : 'docker.removeSubmit')}</button
				>{:else if dialog === 'confirm'}<button
					class="btn btn-warn"
					disabled={busy}
					onclick={() => run(pending)}>{t('common.done')}</button
				>{/if}
		</div>
	</Dialog>{/if}

<style>
	.where {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.menu-app-icon {
		display: inline-flex;
		width: 28px;
		height: 28px;
		vertical-align: middle;
		margin-right: 8px;
	}
	pre {
		font:
			0.8rem/1.6 'IBM Plex Mono',
			monospace;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 65vh;
		overflow: auto;
	}
</style>
