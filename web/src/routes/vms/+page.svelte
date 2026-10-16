<script lang="ts">
	import { storage } from '#lib/storage.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type VM, type VMStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDuration, formatPercent } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { addVmConsole } from '#lib/terminal.svelte.ts';
	import Snapshots from './Snapshots.svelte';
	import VncScreen, { type ScaleMode } from './VncScreen.svelte';

	let status = $state<VMStatus | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let mode = $state<ScaleMode>('scale');
	let sidebar = $state(true);
	let fullscreen = $state(false);
	let dialog = $state<{ kind: 'remove' | 'poweroff' | 'paste' } | { kind: 'xml'; text: string } | null>(null);
	let draft = $state({ deleteDisks: false, confirm: '', text: '' });
	let dialogError = $state<{ text: string; detail: string } | null>(null);
	let screen = $state<VncScreen>();
	let frame = $state<HTMLElement>();

	const vms = $derived(status?.vms ?? []);
	const current = $derived(vms.find((vm) => vm.name === page.url.searchParams.get('vm')) ?? vms[0]);

    const lowSpace = $derived((storage.status?.volumes ?? []).filter(volume => volume.mounted && volume.sizeBytes > 0 && volume.usedBytes / volume.sizeBytes >= 0.9 && current?.disks.some(disk => disk.volume === volume.name || disk.path?.startsWith(`/mnt/${volume.name}/`))).map(volume => volume.name));

	function problem(err: unknown): { text: string; detail: string } {
		const code = err instanceof ApiError ? err.code : 'internal';
		return { text: errorText(code), detail: code === 'vm_failed' && err instanceof ApiError ? err.message : '' };
	}

	async function load() {
		if (document.hidden) return;
		try {
			status = await api.get<VMStatus>('/vms');
		} catch (err) {
			if (!status) error = problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(load, 3000);
		return () => clearInterval(timer);
	});

	async function run(action: () => Promise<unknown>) {
		busy = true;
		error = null;
		try {
			await action();
		} catch (err) {
			error = problem(err);
		}
		await load();
		busy = false;
	}

	const act = (vm: VM, action: string) => run(() => api.post(`/vms/${vm.name}/${action}`));
	const specs = (vm: VM) => t('vms.specs', { vcpus: vm.vcpus, memory: formatBytes(vm.memoryMiB * 1024 * 1024) });

	function detail(vm: VM): string {
		const parts = [t(`vms.template.${vm.template}`), specs(vm), ...vm.disks.map((disk) => `${disk.path?.split('/').at(-1) || disk.file} ${formatBytes(disk.sizeBytes)}`)];
		if (vm.cpuPin?.length) parts.push(t('vms.pinned', { cpus: vm.cpuPin.join(', ') }));
		if (vm.tpm) parts.push('TPM 2.0');
		for (const device of [...(vm.pci ?? []), ...(vm.usb ?? [])]) parts.push(device);
		for (const folder of vm.folders ?? []) parts.push(`${folder.tag} ← ${folder.path}`);
		if (vm.state === 'running') {
			parts.push(t('vms.load', { cpu: formatPercent(vm.cpuPercent), memory: formatBytes(vm.memoryBytes) }));
			const since = new Date(vm.since).getTime();
			if (since > 0) parts.push(t('vms.since', { time: formatDuration(Math.max(0, (Date.now() - since) / 1000)) }));
		}
		return parts.join(' · ');
	}

	function open(kind: 'remove' | 'poweroff' | 'paste') {
		if (!current) return;
		draft = { deleteDisks: false, confirm: '', text: '' };
		dialogError = null;
		dialog = { kind };
	}

	async function showXML() {
		if (!current) return;
		try {
			dialog = { kind: 'xml', text: (await api.get<{ xml: string }>(`/vms/${current.name}/xml`)).xml };
		} catch (err) {
			error = problem(err);
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!current || !dialog) return;
		dialogError = null;
		try {
			if (dialog.kind === 'remove') {
				await api.delete(`/vms/${current.name}`, { deleteDisks: draft.deleteDisks, confirm: draft.confirm });
				await goto('/vms', { replaceState: true });
			} else if (dialog.kind === 'poweroff') {
				await api.post(`/vms/${current.name}/poweroff`);
			} else if (dialog.kind === 'paste') {
				screen?.paste(draft.text);
			}
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		}
	}

	function saveScreenshot() {
		const data = screen?.screenshot();
		if (!data || !current) return;
		const link = document.createElement('a');
		link.href = data;
		link.download = `${current.name}.png`;
		link.click();
	}

	const MODES: ScaleMode[] = ['scale', 'remote', 'full'];
</script>

<svelte:document onfullscreenchange={() => (fullscreen = document.fullscreenElement === frame)} />

{#if error}
	<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
{/if}
{#if status && !status.kvm}
	<p class="notice-warn" role="status">{t('vms.noKvm')}</p>
{/if}

{#if status && vms.length === 0}
	<div class="head-empty">
		<h1>{t('vms.title')}</h1>
		<a class="btn btn-primary" href="/vms/new">{t('vms.create')}</a>
	</div>
	<p class="tile empty">{t('vms.empty')}</p>
{:else if status && current}
	<div class="layout">
		{#if sidebar}
			<aside class="tile" aria-labelledby="vms-title">
				<div class="aside-head">
					<h2 class="tile-title" id="vms-title">{t('vms.title')}</h2>
					<a class="btn add" href="/vms/new" aria-label={t('vms.create')} title={t('vms.create')}>+</a>
				</div>
				{#each vms as vm (vm.name)}
					<a class="vm" href="/vms?vm={vm.name}" aria-current={vm.name === current.name ? 'true' : undefined}>
						<span class="vm-name">{vmLabel(vm)}</span>
						<span class="muted vm-state"><span class="dot" class:ok={vm.state === 'running'} aria-hidden="true"></span>{t(`vms.state.${vm.state}`)} · {specs(vm)}</span>
					</a>
				{/each}
			</aside>
		{/if}

		<section class="main" aria-labelledby="vm-title">
			<div class="head">
				<div class="title">
					<div class="name-row">
						<h1 id="vm-title">{vmLabel(current)}</h1>
						<span class="chip" class:ok={current.state === 'running'}>{t(`vms.state.${current.state}`)}</span>
						{#if current.autostart}<span class="chip">{t('vms.autostart')}</span>{/if}
					</div>
					{#if current.description}<span class="description">{current.description}</span>{/if}
					<span class="muted mono detail">{detail(current)}</span>
				</div>
				<div class="buttons">
					{#if current.state === 'running'}
						<button type="button" class="btn" onclick={() => act(current, 'reboot')} disabled={busy}>{t('vms.reboot')}</button>
						<button type="button" class="btn" onclick={() => act(current, 'shutdown')} disabled={busy}>{t('vms.shutdown')}</button>
						<button type="button" class="btn" onclick={() => open('poweroff')} disabled={busy}>{t('vms.poweroff')}</button>
					{:else}
						<button type="button" class="btn btn-primary" onclick={() => act(current, 'start')} disabled={busy || !current.available || !status.kvm || !!current.job}>{t('vms.start')}</button>
						<button type="button" class="btn" onclick={() => open('remove')} disabled={busy}>{t('vms.remove')}</button>
					{/if}
					<Snapshots vm={current} />
					<a class="btn" href="/vms/edit?vm={current.name}">{t('vms.edit')}</a>
				</div>
			</div>
			{#if lowSpace.length}<p class="notice-warn" role="status">{t('vms.lowSpace', { volumes: lowSpace.join(', ') })}</p>{/if}
            {#if !current.available}<p class="notice-warn" role="status">{t('vms.unavailable')}</p>{/if}
			{#if current.job}
				<div class={current.job.state === 'failed' ? 'notice-error' : 'notice-warn'} role="status">
					{t(current.job.state === 'failed' ? 'vms.jobFailed' : 'vms.jobRunning', { file: current.job.text })}
					{#if current.job.state === 'failed'}<pre>{current.job.text}</pre>{/if}
				</div>
			{/if}

			<div class="frame" class:fullscreen bind:this={frame}>
				{#if current.state === 'running'}
					{#key current.name}
						<VncScreen bind:this={screen} vm={current.name} {mode} />
					{/key}
					{#if fullscreen}
						<div class="fsbar">
							<span class="mono">{vmLabel(current)}</span>
							<button type="button" onclick={() => screen?.ctrlAltDel()}>{t('vms.ctrlAltDel')}</button>
							{#each MODES as entry (entry)}
								<button type="button" aria-pressed={mode === entry} onclick={() => (mode = entry)}>{t(`vms.mode.${entry}`)}</button>
							{/each}
							<button type="button" class="strong" onclick={() => document.exitFullscreen()}>{t('vms.exitFullscreen')}</button>
						</div>
					{/if}
				{:else}
					<div class="off">
						<span>{t('vms.off', { name: vmLabel(current) })}</span>
						<button type="button" class="btn btn-primary" onclick={() => act(current, 'start')} disabled={busy || !current.available || !status.kvm || !!current.job}>{t('vms.startVm')}</button>
					</div>
				{/if}
			</div>
			<div class="toolbar">
				<span class="muted">{t('vms.screen')}</span>
				<span class="grow"></span>
				{#if current.state === 'running'}
					<button type="button" onclick={() => screen?.ctrlAltDel()}>{t('vms.ctrlAltDel')}</button>
					<button type="button" onclick={() => open('paste')}>{t('vms.clipboard')}</button>
					<span class="muted label" id="vm-scale">{t('vms.display')}</span>
					<span class="modes" role="group" aria-labelledby="vm-scale">
						{#each MODES as entry (entry)}
							<button type="button" aria-pressed={mode === entry} title={t(`vms.mode.${entry}.hint`)} onclick={() => (mode = entry)}>{t(`vms.mode.${entry}`)}</button>
						{/each}
					</span>
					<button type="button" onclick={saveScreenshot}>{t('vms.screenshot')}</button>
				{/if}
				<button type="button" aria-pressed={sidebar} onclick={() => (sidebar = !sidebar)}>{t('vms.sidebar')}</button>
				{#if current.state === 'running'}
					<button type="button" onclick={() => addVmConsole(current.name, vmLabel(current))}>{t('vms.serial')}</button>
					<a class="tool" href="/vms/screen?vm={current.name}" target="_blank" rel="noopener" onclick={(event) => { event.preventDefault(); window.open(`/vms/screen?vm=${current.name}`, `vm-${current.name}`, 'popup,width=1296,height=860'); }}>{t('vms.ownWindow')}</a>
					<button type="button" class="strong" onclick={() => frame?.requestFullscreen()}>{t('vms.fullscreen')}</button>
				{/if}
			</div>

			<p class="muted hint">{t('vms.hint')} <button type="button" class="link" onclick={showXML}>{t('vms.showXml')}</button></p>
		</section>
	</div>
{/if}

{#if dialog?.kind === 'xml'}
	<Dialog title={t('vms.showXml')} wide onclose={() => (dialog = null)}>
		<pre class="xml">{dialog.text}</pre>
		<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={() => (dialog = null)}>{t('common.done')}</button></div>
	</Dialog>
{:else if dialog && current}
	{@const kind = dialog.kind}
	<Dialog title={t(`vms.dialog.${kind}`, { name: vmLabel(current) })} onclose={() => (dialog = null)}>
		<form onsubmit={submit}>
			{#if kind === 'remove'}
				<p class="text">{t('vms.removeText')}</p>
				<label class="check"><input type="checkbox" bind:checked={draft.deleteDisks} /><span>{t('vms.deleteDisks')}</span></label>
				{#if draft.deleteDisks}
					<p class="notice-error">{t('vms.deleteDisksWarning', { disks: current.disks.filter((disk) => !disk.path).map((disk) => disk.file).join(', ') || '–' })}</p>
					<label><span>{t('vms.confirmName', { name: vmLabel(current) })}</span><input class="field mono" type="text" bind:value={draft.confirm} autocapitalize="none" spellcheck="false" /></label>
				{/if}
			{:else if kind === 'poweroff'}
				<p class="text">{t('vms.poweroffText')}</p>
			{:else if kind === 'paste'}
				<p class="text">{t('vms.pasteText')}</p>
				<!-- svelte-ignore a11y_autofocus -->
				<textarea class="field mono" rows="5" bind:value={draft.text} spellcheck="false" autofocus></textarea>
			{/if}
			{#if dialogError}
				<div class="notice-error" role="alert">{dialogError.text}{#if dialogError.detail}<pre>{dialogError.detail}</pre>{/if}</div>
			{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
				<button type="submit" class="btn {kind === 'paste' ? 'btn-primary' : 'btn-warn'}" disabled={kind === 'remove' && draft.deleteDisks && draft.confirm !== vmLabel(current)}>{t(`vms.submit.${kind}`)}</button>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	.head-empty,
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.empty {
		padding: 40px 32px;
		color: var(--muted);
		font-size: 0.875rem;
		text-align: center;
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 20px;
	}
	aside {
		flex: 0 1 280px;
		min-width: 220px;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.aside-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 12px 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.add {
		width: 2.25rem;
		height: 2.25rem;
		padding: 0;
		font-size: 1.125rem;
	}
	.vm {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border-left: 3px solid transparent;
		color: var(--text);
		text-decoration: none;
	}
	.vm:hover {
		background: var(--chip);
	}
	.vm[aria-current] {
		border-left-color: var(--accent);
		background: var(--chip);
	}
	.vm-name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.vm-state {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 0.75rem;
	}
	.main {
		flex: 999 1 560px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.name-row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.description {
		font-size: 0.875rem;
	}
	.detail {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.toolbar,
	.fsbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
		font-size: 0.8125rem;
	}
	.grow {
		flex: 1 1 auto;
	}
	.label {
		margin-left: 8px;
	}
	.modes {
		display: inline-flex;
		gap: 2px;
	}
	.toolbar button,
	.fsbar button {
		height: 2.25rem;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.tool {
		height: 2.25rem;
		padding: 0 10px;
		display: inline-flex;
		align-items: center;
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.tool:hover {
		background: var(--chip);
	}
	.toolbar button:hover,
	.toolbar button[aria-pressed='true'],
	.fsbar button:hover,
	.fsbar button[aria-pressed='true'] {
		background: var(--chip);
	}
	.toolbar .strong,
	.fsbar .strong {
		border: 1px solid var(--border);
	}
	/* The window of the console: drag its lower right corner. */
	.frame {
		position: relative;
		width: 100%;
		max-width: 100%;
		aspect-ratio: 16 / 10;
		min-width: 320px;
		min-height: 200px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		overflow: hidden;
		resize: both;
	}
	.frame.fullscreen {
		border: 0;
		border-radius: 0;
		resize: none;
	}
	.fsbar {
		position: absolute;
		bottom: 10px;
		left: 50%;
		transform: translateX(-50%);
		padding: 4px 10px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
		opacity: 0.25;
		transition: opacity 0.15s ease;
	}
	.fsbar:hover,
	.fsbar:focus-within {
		opacity: 1;
	}
	.off {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 14px;
		color: var(--muted);
		font-size: 0.9375rem;
	}
	.hint {
		font-size: 0.75rem;
	}
	.link {
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.8125rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
	.xml {
		max-height: 60vh;
		margin: 0;
		padding: 12px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
</style>
