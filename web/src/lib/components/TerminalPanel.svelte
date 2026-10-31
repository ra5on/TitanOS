<script lang="ts">
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	// The terminal at the lower edge of every page: a root shell on the NAS
	// and consoles of containers as tabs. The page above stays usable.
	import { api, type DockerStatus, type VMStatus } from '#lib/api.ts';
	import { formatDuration } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session as account } from '#lib/session.svelte.ts';
	import {
		addContainerConsole,
		addNasShell,
		addVmConsole,
		closeSession,
		MIN_HEIGHT,
		remember,
		terminal,
	} from '#lib/terminal.svelte.ts';
	import TerminalScreen from './TerminalScreen.svelte';
	import Dialog from './Dialog.svelte';
	import { focusFirstItem, moveFocus } from './menu.ts';
	import { tick } from 'svelte';

	type TerminalInfo = { enabled: boolean; nasShell: boolean };
	let info = $state<TerminalInfo | null>(null);
	let error = $state('');
	let menu = $state(false);
	let containers = $state<{ id: string; name: string }[]>([]);
	let machines = $state<{ name: string; label: string }[]>([]);
	let searching = $state(false);
	let query = $state('');
	let now = $state(Date.now());
	let screens = $state<Record<number, TerminalScreen | undefined>>({});
	let menuRoot = $state<HTMLElement>();
	let menuPanel = $state<HTMLElement>();
	let plus = $state<HTMLButtonElement>();
	// A connected root shell that "✕" would end, while its confirmation is open.
	let ending = $state<{ id: number; title: string } | null>(null);

	const current = $derived(terminal.sessions.find((session) => session.id === terminal.active));
	const screen = $derived(current ? screens[current.id] : undefined);

	/** Asks titand whether terminals are allowed; false if a session has no reason to wait. */
	// No console asks for the password again: the login is enough.
	async function check(): Promise<boolean> {
		try {
			info = await api.get<TerminalInfo>('/terminal');
			error = '';
			return !info?.enabled;
		} catch (err) {
			error = apiErrorText(err);
			return false;
		}
	}

	// Asked whenever the panel opens: both can have changed in the meantime.
	$effect(() => {
		if (terminal.open) check();
	});

	// A first session, so that an opened panel is not empty.
	$effect(() => {
		if (terminal.open && info?.enabled && info.nasShell && terminal.sessions.length === 0)
			addNasShell();
	});

	$effect(() => {
		if (!terminal.open) return;
		const timer = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(timer);
	});

	async function openMenu() {
		menu = !menu;
		if (!menu) return;
		try {
			const status = await api.get<DockerStatus>('/docker');
			containers = status.containers
				.filter((container) => container.state === 'running')
				.map((container) => ({ id: container.id, name: dockerName(container, 'distinct') }));
		} catch {
			containers = [];
		}
		try {
			machines = (await api.get<VMStatus>('/vms')).vms
				.filter((vm) => vm.state === 'running')
				.map((vm) => ({ name: vm.name, label: vmLabel(vm) }));
		} catch {
			machines = [];
		}
		await tick();
		if (menu) focusFirstItem(menuPanel);
	}

	function closeMenu(focus = false) {
		menu = false;
		if (focus) plus?.focus();
	}

	function menuKeys(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			closeMenu(true);
			return;
		}
		moveFocus(menuPanel, event);
	}

	// Ending a root shell that is still connected asks first; anything else closes at once.
	function end(id: number, title: string, kind: string) {
		if (kind === 'nas' && screens[id]?.connected()) ending = { id, title };
		else closeSession(id);
	}

	function choose(action: () => void) {
		menu = false;
		action();
	}

	function setFont(size: number) {
		terminal.fontSize = Math.min(18, Math.max(11, size));
		remember();
	}

	const maxHeight = () => Math.max(MIN_HEIGHT, window.innerHeight - 140);

	function drag(event: PointerEvent) {
		const handle = event.currentTarget as HTMLElement;
		handle.setPointerCapture(event.pointerId);
		const move = (moved: PointerEvent) =>
			(terminal.height = Math.min(
				maxHeight(),
				Math.max(MIN_HEIGHT, window.innerHeight - moved.clientY),
			));
		const stop = () => {
			handle.removeEventListener('pointermove', move);
			remember();
		};
		handle.addEventListener('pointermove', move);
		handle.addEventListener('pointerup', stop, { once: true });
		handle.addEventListener('pointercancel', stop, { once: true });
	}

	function resizeByKey(event: KeyboardEvent) {
		if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
		event.preventDefault();
		terminal.height = Math.min(
			maxHeight(),
			Math.max(MIN_HEIGHT, terminal.height + (event.key === 'ArrowUp' ? 40 : -40)),
		);
		remember();
	}

	function status(): string {
		if (!current) return '';
		const time = formatDuration(Math.max(0, (now - current.started) / 1000));
		return current.kind === 'nas'
			? t('terminal.statusNas', { time })
			: t(
					current.kind === 'vm'
						? 'terminal.statusVm'
						: current.kind === 'lxc'
							? 'terminal.statusLxc'
							: 'terminal.statusContainer',
					{ name: current.title, time },
				);
	}
</script>

<svelte:window
	onclick={(event) => menu && !menuRoot?.contains(event.target as Node) && (menu = false)}
/>

<section
	class="panel"
	class:full={terminal.full}
	class:closed={!terminal.open}
	style:height={terminal.full ? undefined : `${terminal.height}px`}
	aria-label={t('terminal.title')}
	aria-hidden={!terminal.open}
>
	{#if !terminal.full}
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<div
			class="grip"
			role="separator"
			aria-orientation="horizontal"
			aria-label={t('terminal.resize')}
			aria-valuenow={terminal.height}
			tabindex="0"
			onpointerdown={drag}
			onkeydown={resizeByKey}
		>
			<span></span>
		</div>
	{/if}

	<div class="toolbar">
		<div class="tabs" role="tablist" aria-label={t('terminal.sessions')} bind:this={menuRoot}>
			{#each terminal.sessions as session (session.id)}
				<div class="tab" class:on={session.id === terminal.active}>
					<button
						type="button"
						role="tab"
						aria-selected={session.id === terminal.active}
						onclick={() => (terminal.active = session.id)}
					>
						<span
							class="kind"
							class:ct={session.kind === 'ct'}
							class:vm={session.kind === 'vm'}
							class:lxc={session.kind === 'lxc'}
							aria-hidden="true"
							>{session.kind === 'nas'
								? 'NAS'
								: session.kind === 'vm'
									? 'VM'
									: session.kind === 'lxc'
										? 'LXC'
										: 'CT'}</span
						>
						<span class="mono">{session.title}</span>
					</button>
					<button
						type="button"
						class="x"
						aria-label={t('terminal.end', { name: session.title })}
						onclick={() => end(session.id, session.title, session.kind)}>✕</button
					>
				</div>
			{/each}
			<button
				type="button"
				class="plus"
				bind:this={plus}
				aria-label={t('terminal.new')}
				aria-haspopup="menu"
				aria-expanded={menu}
				onclick={openMenu}
				disabled={!info?.enabled}>+</button
			>
			{#if menu}
				<div
					class="menu floating-menu"
					role="menu"
					tabindex="-1"
					aria-label={t('terminal.new')}
					bind:this={menuPanel}
					onkeydown={menuKeys}
				>
					{#if info?.nasShell}
						<button type="button" role="menuitem" onclick={() => choose(addNasShell)}
							><span class="kind" aria-hidden="true">NAS</span>{t('terminal.newNas')}</button
						>
					{/if}
					<span class="muted heading">{t('terminal.newContainer')}</span>
					{#each containers as container (container.id)}
						<button
							type="button"
							role="menuitem"
							onclick={() => choose(() => addContainerConsole(container.id, container.name))}
							><span class="dot ok" aria-hidden="true"></span><span class="mono"
								>{container.name}</span
							></button
						>
					{:else}
						<span class="muted none">{t('terminal.noContainers')}</span>
					{/each}
					{#if machines.length > 0}
						<span class="muted heading">{t('terminal.newVm')}</span>
						{#each machines as machine (machine.name)}
							<button
								type="button"
								role="menuitem"
								onclick={() => choose(() => addVmConsole(machine.name, machine.label))}
								><span class="kind vm" aria-hidden="true">VM</span><span class="mono"
									>{machine.label}</span
								></button
							>
						{/each}
					{/if}
					<a role="menuitem" href="/vms" onclick={() => closeMenu()}
						><span class="kind vm" aria-hidden="true">VM</span>{t('terminal.vmConsole')}</a
					>
				</div>
			{/if}
		</div>

		{#if current?.kind === 'ct'}
			<label class="shell"
				><span class="muted">{t('terminal.shell')}</span>
				<select class="field mono" bind:value={current.shell}>
					<option value="">{t('terminal.shellAuto')}</option>
					<option value="bash">bash</option>
					<option value="sh">sh</option>
				</select>
			</label>
			<label class="shell"
				><span class="muted">{t('terminal.user')}</span>
				<input
					class="field mono user"
					list="terminal-users"
					value={current.user ?? ''}
					placeholder={t('terminal.userDefault')}
					spellcheck="false"
					autocapitalize="off"
					onchange={(event) => {
						const value = event.currentTarget.value.trim();
						if (current && value !== (current.user ?? '')) current.user = value || undefined;
					}}
				/>
				<datalist id="terminal-users"
					><option value="root"></option><option value="1000:1000"></option></datalist
				>
			</label>
		{/if}

		<div class="tools">
			{#if searching}
				<!-- svelte-ignore a11y_autofocus -->
				<input
					class="field mono find"
					type="search"
					bind:value={query}
					placeholder={t('terminal.search')}
					aria-label={t('terminal.search')}
					autofocus
					onkeydown={(event) => {
						if (event.key === 'Enter') screen?.find(query, event.shiftKey);
						if (event.key === 'Escape') {
							searching = false;
							screen?.focus();
						}
					}}
				/>
			{/if}
			<button
				type="button"
				aria-label={t('terminal.search')}
				aria-pressed={searching}
				onclick={() => (searching = !searching)}>⌕</button
			>
			<button type="button" onclick={() => screen?.copy()}>{t('terminal.copy')}</button>
			<button type="button" onclick={() => screen?.paste()}>{t('terminal.paste')}</button>
			<button
				type="button"
				aria-label={t('terminal.fontSmaller')}
				onclick={() => setFont(terminal.fontSize - 1)}>A−</button
			>
			<span class="mono size">{terminal.fontSize}</span>
			<button
				type="button"
				aria-label={t('terminal.fontLarger')}
				onclick={() => setFont(terminal.fontSize + 1)}>A+</button
			>
			{#if !terminal.full}
				<a
					class="tool"
					href="/terminal"
					target="_blank"
					rel="noopener"
					aria-label={t('terminal.ownTab')}
					title={t('terminal.ownTab')}>↗</a
				>
				<button
					type="button"
					aria-label={t('terminal.minimize')}
					title={t('terminal.minimize')}
					onclick={() => (terminal.open = false)}>▾</button
				>
			{/if}
		</div>
	</div>

	<div class="body">
		{#if info && !info.enabled}
			<p class="message">{t('terminal.off')} <a href="/settings">{t('nav.settings')}</a></p>
		{:else if terminal.sessions.length === 0}
			<p class="message">
				{error || t(info?.nasShell === false ? 'terminal.emptyNoShell' : 'terminal.empty')}
			</p>
		{/if}
		{#each terminal.sessions as session (session.id)}
			{#key `${session.shell}|${session.user ?? ''}`}
				<TerminalScreen
					bind:this={screens[session.id]}
					{session}
					active={terminal.open && session.id === terminal.active}
					fontSize={terminal.fontSize}
					confirmed={info?.enabled === true}
					onrefused={() => check()}
				/>
			{/key}
		{/each}
	</div>

	<div class="foot mono">
		<span>{status()}</span>
		<span>{t('terminal.footer', { name: account.data?.user?.name ?? '' })}</span>
	</div>
</section>

{#if ending}
	{@const target = ending}
	<Dialog title={t('terminal.endTitle')} onclose={() => (ending = null)}>
		<p>{t('terminal.endText', { name: target.title })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ending = null)}>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				onclick={() => {
					closeSession(target.id);
					ending = null;
				}}>{t('terminal.endConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.panel {
		position: fixed;
		/* Beside the sidebar ("Navigation: side"), not below it. */
		left: var(--side-nav-width, 0px);
		right: 0;
		bottom: 0;
		z-index: 15;
		display: flex;
		flex-direction: column;
		background: var(--bg);
		border-top: 1px solid var(--border);
		box-shadow: var(--shadow-menu);
	}
	.panel.full {
		top: var(--header-height, 60px);
	}
	/* Hidden, not removed: the sessions go on. */
	.panel.closed {
		display: none;
	}
	.grip {
		height: 10px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: ns-resize;
		touch-action: none;
	}
	.grip span {
		width: 44px;
		height: 4px;
		border-radius: var(--radius-bar);
		background: var(--border);
	}
	.toolbar {
		flex: none;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 0 16px 8px;
		border-bottom: 1px solid var(--border);
	}
	.panel.full .toolbar {
		padding-top: 8px;
	}
	.tabs {
		position: relative;
		flex: 1 1 320px;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
	}
	.tab {
		display: flex;
		align-items: center;
		height: 2.375rem;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
	}
	.tab.on {
		background: var(--surface);
		border-color: var(--border);
		color: var(--text);
	}
	.tab button {
		display: flex;
		align-items: center;
		gap: 8px;
		height: 100%;
		padding: 0 6px 0 12px;
		background: transparent;
		border: 0;
		color: inherit;
		font: inherit;
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.tab .x {
		padding: 0 10px 0 4px;
		color: var(--muted);
	}
	.kind {
		padding: 1px 6px;
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		border-radius: var(--radius-small);
		font-size: var(--text-xs);
		font-weight: 600;
	}
	.kind.ct {
		background: var(--ok-bg);
		color: var(--ok-text);
	}
	.kind.vm {
		background: var(--warn-bg);
		color: var(--warn-text);
	}
	.kind.lxc {
		background: var(--chip);
		color: var(--text);
	}
	.plus {
		width: 2.375rem;
		height: 2.375rem;
		background: transparent;
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-lg);
		cursor: pointer;
	}
	.menu {
		position: absolute;
		bottom: calc(100% + 8px);
		left: 0;
		z-index: 6;
		width: 280px;
		max-height: 50vh;
		overflow-y: auto;
		padding: 6px;
		display: flex;
		flex-direction: column;
	}
	.panel.full .menu {
		top: calc(100% + 8px);
		bottom: auto;
	}
	.menu button,
	.menu a {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 2.5rem;
		padding: 0 12px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.menu a {
		margin-top: 6px;
		border-top: 1px solid var(--border);
		border-radius: 0;
		text-decoration: none;
	}
	.menu button:hover,
	.menu a:hover {
		background: var(--chip);
	}
	.heading,
	.none {
		padding: 6px 12px;
		font-size: var(--text-sm);
	}
	.shell {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: var(--text-sm);
	}
	.shell .user {
		width: 8rem;
	}
	.shell select {
		width: auto;
		height: 2.125rem;
		font-size: var(--text-sm);
	}
	.tools {
		display: flex;
		align-items: center;
		gap: 2px;
	}
	.tools button,
	.tool {
		min-width: 2.25rem;
		height: 2.25rem;
		padding: 0 8px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-decoration: none;
		cursor: pointer;
	}
	.tools button:hover,
	.tools button[aria-pressed='true'],
	.tool:hover {
		background: var(--chip);
	}
	.find {
		width: 12rem;
		height: 2.125rem;
		font-size: var(--text-sm);
	}
	.size {
		width: 1.75rem;
		font-size: var(--text-sm);
		color: var(--muted);
		text-align: center;
	}
	.body {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
	}
	.message {
		position: absolute;
		inset: 0;
		z-index: 2;
		padding: 20px 24px;
		background: var(--bg);
		font-size: var(--text-md);
	}
	.foot {
		flex: none;
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 4px 20px;
		padding: 6px 20px;
		border-top: 1px solid var(--border);
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
