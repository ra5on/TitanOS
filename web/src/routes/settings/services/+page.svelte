<script lang="ts">
	// Settings › Services (decided 31.10.2026): Docker, VMs and system
	// containers can be switched off; the service then does not run and its
	// tab is hidden. Data always stays.
	//
	// PLAN 73 (drafts 08/09, owner 8.11.2026): "Start nach dem Hochfahren" –
	// apps and VMs start only when TitanOS is ready (volumes, shares over
	// SMB/NFS) plus an extra wait, in an order with a wait after each entry.
	import PageHead from '#lib/components/PageHead.svelte';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { api, type DockerStatus, type VMStatus } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { loadServices } from '#lib/session.svelte.ts';

	type Service = { enabled: boolean; running: number; total: number; available: boolean };
	type Name = 'docker' | 'vms' | 'lxc';
	const NAMES: Name[] = ['docker', 'vms', 'lxc'];
	const ICONS: Record<Name, IconName> = { docker: 'box', vms: 'monitor', lxc: 'terminal' };

	let services = $state<Partial<Record<Name, Service>>>({});
	let error = $state('');
	let busy = $state<Name | ''>('');
	// Switching off with something running asks first.
	let asking = $state<Name | ''>('');

	type Entry = { kind: 'vm' | 'app'; name: string; wait: number; off?: boolean };
	type Startup = { noWait?: boolean; extra: number; order: Entry[] };
	let startup = $state<Startup | null>(null);
	let saved = $state('');
	let apps = $state<string[]>([]);
	let vms = $state<{ name: string; autostart: boolean }[]>([]);
	let adding = $state('');
	const WAITS = [0, 5, 10, 20, 30, 45, 60, 90, 120, 180, 300];
	const EXTRAS = [0, 10, 20, 30, 60, 120, 300];

	async function load() {
		try {
			services = await api.get<Partial<Record<Name, Service>>>('/services');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function loadStartup() {
		try {
			const answer = await api.get<{ settings: Startup }>('/system/startup');
			startup = answer.settings;
			saved = JSON.stringify(startup);
		} catch {
			startup = null;
		}
		api
			.get<DockerStatus>('/docker')
			.then(
				(d) => (apps = [...new Set((d.containers ?? []).map((c) => c.app).filter(Boolean))].sort()),
			)
			.catch(() => (apps = []));
		api
			.get<VMStatus>('/vms')
			.then((v) => (vms = (v.vms ?? []).map((m) => ({ name: m.name, autostart: m.autostart }))))
			.catch(() => (vms = []));
	}
	$effect(() => {
		void load();
		void loadStartup();
	});

	async function set(name: Name, enabled: boolean) {
		busy = name;
		error = '';
		asking = '';
		try {
			services = await api.put<Partial<Record<Name, Service>>>(`/services/${name}`, { enabled });
			await loadServices();
		} catch (e) {
			error = apiErrorText(e);
			await load();
		} finally {
			busy = '';
		}
	}
	function toggle(name: Name, enabled: boolean) {
		const service = services[name];
		if (!enabled && service && service.running > 0) {
			asking = name;
			return;
		}
		void set(name, enabled);
	}

	const dirty = $derived(!!startup && JSON.stringify(startup) !== saved);
	let saving = $state(false);
	async function saveStartup() {
		if (!startup) return;
		saving = true;
		error = '';
		try {
			startup = await api.put<Startup>('/system/startup', $state.snapshot(startup));
			saved = JSON.stringify(startup);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}
	function move(i: number, by: number) {
		if (!startup) return;
		const j = i + by;
		if (j < 0 || j >= startup.order.length) return;
		const order = [...startup.order];
		[order[i], order[j]] = [order[j], order[i]];
		startup.order = order;
	}
	const missing = $derived(
		startup
			? [
					...vms.map((v) => ({ kind: 'vm' as const, name: v.name })),
					...apps.map((a) => ({ kind: 'app' as const, name: a })),
				].filter((x) => !startup!.order.some((e) => e.kind === x.kind && e.name === x.name))
			: [],
	);
	function add() {
		if (!startup || !adding) return;
		const [kind, name] = adding.split('/', 2) as ['vm' | 'app', string];
		startup.order = [...startup.order, { kind, name, wait: 0 }];
		adding = '';
	}
	const vmAutostart = (name: string) => vms.find((v) => v.name === name)?.autostart ?? false;
</script>

<PageHead title={t('services.title')}>
	<p class="muted">{t('services.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

<!-- PLAN 78-1: every service is a row with its state and its switch. -->
<SettingGroup title={t('services.title')} id="sv-services">
	{#each NAMES as name (name)}
		{@const service = services[name]}
		{#if service}
			<SettingRow icon={ICONS[name]} title={t(`services.${name}` as MessageKey)} for="sv-{name}">
				{#snippet description()}
					{t(`services.${name}Desc` as MessageKey)}
					<span class="state"
						>{#if !service.available}{t('services.unavailable')}{:else if service.enabled}{t(
								'services.counts',
								{ running: service.running, total: service.total },
							)}{:else}{t('services.offHint')}{/if}</span
					>
				{/snippet}
				{#snippet control()}
					<span class="chip {service.enabled ? 'ok' : ''}"
						>{service.enabled ? '● ' + t('services.on') : '○ ' + t('services.off')}</span
					>
					<input
						id="sv-{name}"
						type="checkbox"
						class="switch"
						role="switch"
						checked={service.enabled}
						disabled={busy !== '' || (!service.available && !service.enabled)}
						onchange={(e) => {
							const on = e.currentTarget.checked;
							e.currentTarget.checked = service.enabled;
							toggle(name, on);
						}}
					/>
				{/snippet}
			</SettingRow>
			{#if asking === name}
				<div class="notice-warn ask" role="alert">
					<p>
						{t('services.confirm', {
							name: t(`services.${name}` as MessageKey),
							count: service.running,
						})}
					</p>
					<div class="row-actions">
						<button type="button" class="btn btn-danger" onclick={() => set(name, false)}
							>{t('services.switchOff', { name: t(`services.${name}` as MessageKey) })}</button
						>
						<button type="button" class="btn" onclick={() => (asking = '')}
							>{t('common.cancel')}</button
						>
					</div>
				</div>
			{/if}
		{/if}
	{/each}
</SettingGroup>

{#if startup}
	<!-- The start after booting is one form with one Save (the bar below):
	     the request carries the waits and the whole order. -->
	<SettingGroup title={t('startup.title')} id="sv-start">
		<SettingRow
			icon="power"
			title={t('startup.waitReady')}
			desc={t('startup.waitReadyHint')}
			for="sv-wait-ready"
		>
			{#snippet control()}
				<input
					id="sv-wait-ready"
					type="checkbox"
					class="switch"
					role="switch"
					checked={!startup!.noWait}
					onchange={(e) => (startup!.noWait = !e.currentTarget.checked)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="clock"
			title={t('startup.extra')}
			desc={t('startup.extraHint')}
			for="sv-extra"
		>
			{#snippet control()}
				<select id="sv-extra" class="field" disabled={startup!.noWait} bind:value={startup!.extra}>
					{#each EXTRAS as s (s)}<option value={s}>{t('startup.seconds', { count: s })}</option
						>{/each}
				</select>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('startup.order')} id="sv-order" desc={t('startup.orderHint')}>
		{#each startup.order as entry, i (entry.kind + '/' + entry.name)}
			<SettingRow
				icon={entry.kind === 'vm' ? 'monitor' : 'box'}
				title="{i + 1}. {entry.name}"
				desc={t(entry.kind === 'vm' ? 'startup.kindVm' : 'startup.kindApp') +
					(entry.kind === 'vm' && !vmAutostart(entry.name)
						? ' · ' + t('startup.vmAutostartOff')
						: '')}
			>
				{#snippet control()}
					<button
						type="button"
						class="btn icon"
						aria-label={t('startup.up', { name: entry.name })}
						disabled={i === 0}
						onclick={() => move(i, -1)}><Icon name="arrowUp" size={14} /></button
					>
					<button
						type="button"
						class="btn icon"
						aria-label={t('startup.down', { name: entry.name })}
						disabled={i === startup!.order.length - 1}
						onclick={() => move(i, 1)}><Icon name="chevronDown" size={14} /></button
					>
					<label class="wait"
						><span>{t('startup.thenWait')}</span><select class="field" bind:value={entry.wait}>
							{#each WAITS as s (s)}<option value={s}>{t('startup.seconds', { count: s })}</option
								>{/each}
						</select></label
					>
					{#if entry.kind === 'app'}<input
							type="checkbox"
							class="switch"
							role="switch"
							aria-label={t('startup.autostart', { name: entry.name })}
							title={t('startup.autostart', { name: entry.name })}
							checked={!entry.off}
							onchange={(e) => (entry.off = !e.currentTarget.checked)}
						/>{/if}
					<button
						type="button"
						class="btn icon"
						aria-label={t('startup.remove', { name: entry.name })}
						onclick={() => (startup!.order = startup!.order.filter((_, k) => k !== i))}
						><Icon name="close" size={14} /></button
					>
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('startup.empty')}</p>
		{/each}
		{#if missing.length}
			<div class="row-actions add">
				<select class="field" aria-label={t('startup.add')} bind:value={adding}>
					<option value="">{t('startup.add')}</option>
					{#each missing as m (m.kind + '/' + m.name)}<option value={m.kind + '/' + m.name}
							>{m.name} – {t(m.kind === 'vm' ? 'startup.kindVm' : 'startup.kindApp')}</option
						>{/each}
				</select>
				<button type="button" class="btn" disabled={!adding} onclick={add}
					>{t('startup.addButton')}</button
				>
			</div>
		{/if}
	</SettingGroup>

	{#if dirty || saving}
		<div class="row-actions sticky">
			<span class="grow muted">{t('agent.unsaved')}</span>
			<button type="button" class="btn" disabled={saving} onclick={loadStartup}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={saving} onclick={saveStartup}
				>{t('common.save')}</button
			>
		</div>
	{/if}
{/if}

<style>
	.state {
		display: block;
	}
	.ask p {
		margin: 0 0 var(--space-2);
	}
	/* The controls of an entry of the order: a narrow select for the wait. */
	.wait {
		white-space: nowrap;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.wait select.field {
		width: auto;
		min-width: 6.5rem;
	}
	.add {
		margin-top: var(--space-1-5);
	}
	.add select {
		flex: 1 1 12rem;
		min-width: 0;
	}
	.btn.icon {
		width: 2rem;
		height: 2rem;
		padding: 0;
		justify-content: center;
	}
	.row-actions .grow {
		flex: 1;
	}
</style>
