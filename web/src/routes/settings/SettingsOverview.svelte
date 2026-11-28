<script lang="ts">
	// Systemsteuerung › Übersicht. §121 (owner 28.11.2026: „Noch schlichter“,
	// draft Entwuerfe/121-desktop/1-uebersicht-v4): a head with the NAS and
	// one button "Ein/Aus", then two groups of the same rows – "Zustand"
	// (volumes and the live values in one row) and "Zu erledigen". Services,
	// the last changes and the shortcuts are on their own pages. Only real
	// answers of the NAS; what an account may not read is not asked for.
	import { onMount } from 'svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes, formatDuration, formatPercent, osVersion } from '#lib/format.ts';
	import { live, watchLive } from '#lib/live-usage.svelte.ts';
	import { settingsAreas } from '#lib/settings-search.ts';
	import { settingsStates } from '#lib/settings-states.svelte.ts';
	import { noticeLink } from '#lib/notice-link.ts';
	import { checkSetup, type SetupTodo } from '#lib/setup-checklist.ts';
	import { logout } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	let { admin, prefix = 'overview' }: { admin: boolean; prefix?: string } = $props();

	const info = $derived(system.info);
	// Accounts with the role "user" get no volumes from the NAS: the row says nothing of them.
	const volumes = $derived(storage.status?.volumes ?? null);
	const bad = $derived((volumes ?? []).filter((v) => v.state !== 'ok').length);
	const address = $derived(info?.network?.ipv4?.split('/')[0] ?? '');
	const v = $derived(live.value);

	const areaLabel = (id: string) => {
		const area = settingsAreas.find((a) => a.id === id);
		return area ? t(area.label) : id;
	};

	// "Zu erledigen": the states of the settings (update, battery, certificate,
	// overdue backup; read once by the settings layout) and the open steps of
	// the setup list of /welcome.
	const states = $derived(Object.entries(settingsStates()));
	let setup = $state<SetupTodo[] | null>(null);
	let setupFailed = $state(false);
	let failedBackups = $state<string[]>([]);
	const open = $derived((setup ?? []).filter((todo) => todo.done === false));
	let power = $state<'restart' | 'shutdown' | null>(null);

	onMount(() => {
		void checkSetup(admin).then((result) => {
			setup = result.todos;
			setupFailed = result.failed;
			failedBackups = result.failedBackups;
		});
		return watchLive(false);
	});

	// In a window the desktop shows the dialog: it has to stay while the NAS prepares.
	function askPower(action: 'restart' | 'shutdown') {
		if (inWindow)
			window.parent.postMessage({ type: 'titanos-desk-dialog', dialog: action }, location.origin);
		else power = action;
	}
</script>

<div class="overview">
	<div class="head">
		<div class="who">
			<p class="name">{info?.hostname ?? ''}</p>
			{#if info}<p class="facts">
					{[
						`TitanOS ${osVersion(info.version)}`,
						info.cpu.model,
						address,
						t('start.uptime', { time: formatDuration(info.uptimeSeconds) }),
					]
						.filter(Boolean)
						.join(' · ')}
				</p>{/if}
		</div>
		<Dropdown label={t('overview.power')} text={t('overview.power')} icon="power" align="right">
			{#snippet children(close)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						void logout();
					}}><Icon name="logout" size={16} />{t('desktop.menu.logout')}</button
				>
				{#if admin}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							askPower('restart');
						}}><Icon name="restart" size={16} />{t('desktop.menu.restart')}</button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							askPower('shutdown');
						}}><Icon name="power" size={16} />{t('desktop.menu.shutdown')}</button
					>
				{/if}
			{/snippet}
		</Dropdown>
	</div>

	<SettingGroup title={t('settingshome.health')} id="{prefix}-health">
		<SettingRow
			href={admin ? '/storage' : undefined}
			icon={bad ? 'info' : 'check'}
			title={!info
				? t('settingshome.health.loading')
				: bad
					? t('overview.attention')
					: t('overview.allGood')}
			desc={volumes
				? volumes.length
					? bad
						? t('settingshome.health.attention')
						: t('overview.volumesOk', { count: volumes.length })
					: t('settingshome.health.noVolume')
				: undefined}
		/>
		<SettingRow href="/tools/taskmanager" icon="activity" title={t('live.title')}>
			{#snippet control()}
				{#if v}
					<span class="values">
						<span>{t('live.cpu')} <b>{formatPercent(v.cpu)}</b></span>
						<span
							>{t('start.ram')}
							<b
								>{t('start.usedOf', {
									used: formatBytes(v.memoryUsed),
									total: formatBytes(v.memoryTotal),
								})}</b
							></span
						>
						{#if v.storageTotal}<span
								>{t('live.storage')}
								<b>{formatPercent((v.storageUsed / v.storageTotal) * 100)}</b></span
							>{/if}
						{#if v.temperature !== null}<span
								>{t('overview.temperature')} <b>{Math.round(v.temperature)} °C</b></span
							>{/if}
					</span>
				{/if}
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('settingshome.todo')} id="{prefix}-todo">
		{#each failedBackups as job (job)}
			<SettingRow
				href={noticeLink('backup_failed', job) ?? undefined}
				icon="archive"
				title={job}
				desc={t('error.backup_failed')}
			>
				{#snippet control()}<span class="chip error">{t('backup.failed')}</span>{/snippet}
			</SettingRow>
		{/each}
		{#each states as [area, state] (area)}
			<SettingRow href="/settings/{area}" icon="bell" title={areaLabel(area)}>
				{#snippet control()}<span class="chip {state.tone}">{t(state.text)}</span>{/snippet}
			</SettingRow>
		{/each}
		{#each open as todo (todo.key)}
			<SettingRow
				href={todo.href}
				icon="list"
				title={t(todo.key)}
				desc={t(`${todo.key}Text` as MessageKey)}
			/>
		{/each}
		{#if setup === null}
			<p class="note muted" role="status">{t('settingshome.todo.checking')}</p>
		{:else if !states.length && !open.length && !failedBackups.length && !setupFailed}
			<div role="status"><SettingRow icon="check" title={t('settingshome.todo.none')} /></div>
		{/if}
		{#if setupFailed}<p class="note muted">{t('settingshome.todo.unknown')}</p>{/if}
	</SettingGroup>
</div>
{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}

<style>
	.overview {
		display: flex;
		flex-direction: column;
		gap: var(--gap, var(--space-4));
		min-width: 0;
	}
	.head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
		flex-wrap: wrap;
	}
	.who {
		min-width: 0;
	}
	.name {
		margin: 0;
		font-size: 1.5rem;
		font-weight: 700;
		letter-spacing: -0.02em;
		overflow-wrap: anywhere;
	}
	.facts {
		margin: 4px 0 0;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.values {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 4px 18px;
		color: var(--muted);
	}
	.values b {
		color: var(--text);
		font-weight: 600;
		white-space: nowrap;
	}
	.note {
		margin: 0;
		font-size: var(--text-base);
	}
	.muted {
		color: var(--muted);
	}
	.chip {
		white-space: normal;
	}
</style>
