<script lang="ts">
	import { api } from '#lib/api.ts';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { MonitorStatus, Operations } from '#lib/operations.ts';
	import { isActive, needsAttention, taskItems } from '#lib/operations/tasks.ts';
	import { toolsAnchors, toolsGroups } from '#lib/tools-nav.ts';
	import type { ScriptsStatus } from './scripts/scripts.ts';

	// "At a glance" shows only what existing answers say, read once when the
	// page opens (no timer). A part that cannot be read shows a dash.
	let operations = $state<Operations | null | undefined>(undefined);
	let scripts = $state<ScriptsStatus | null | undefined>(undefined);
	let monitoring = $state<MonitorStatus | null | undefined>(undefined);

	onMount(() => {
		// Saved links pointed at sections of this page that are pages now.
		const moved = toolsAnchors[location.hash];
		if (moved) {
			void goto(`${moved}${page.url.search}`, { replaceState: true });
			return;
		}
		const read = <T,>(path: string, set: (value: T | null) => void) =>
			api.get<T>(path).then(set, () => set(null));
		void read<Operations>('/operations', (v) => (operations = v));
		void read<ScriptsStatus>('/scripts', (v) => (scripts = v));
		void read<MonitorStatus>('/monitoring', (v) => (monitoring = v));
	});

	const earliest = (dates: (string | null | undefined)[]) =>
		dates
			.filter((d): d is string => !!d && !Number.isNaN(Date.parse(d)))
			.sort((a, b) => Date.parse(a) - Date.parse(b))[0];
	const many = (count: number, one: MessageKey, more: MessageKey) =>
		count === 1 ? t(one) : t(more, { count });

	const tasksLine = $derived.by(() => {
		if (!operations) return operations === null ? '–' : '';
		const tasks = taskItems(operations);
		const running = tasks.filter(isActive).length;
		const failed = tasks.filter(needsAttention).length;
		const parts = [
			running
				? many(running, 'tools.glance.runningOne', 'tools.glance.runningMany')
				: t('tools.glance.idle'),
		];
		if (failed) parts.push(many(failed, 'tools.glance.attentionOne', 'tools.glance.attentionMany'));
		return parts.join(' · ');
	});
	const maintenanceLine = $derived.by(() => {
		if (!operations) return operations === null ? '–' : '';
		const status = operations.maintenance;
		if (!status) return '–';
		if (!status.settings.enabled) return t('tools.glance.off');
		const parts = [
			t('tools.glance.on'),
			many(status.settings.jobs.length, 'tools.glance.jobsOne', 'tools.glance.jobsMany'),
		];
		const next = earliest(status.jobs.map((job) => job.next));
		if (next)
			parts.push(
				t('tools.glance.next', {
					time: formatDateTime(new Date(next), status.settings.timezone || undefined),
				}),
			);
		return parts.join(' · ');
	});
	const scriptsLine = $derived.by(() => {
		if (!scripts) return scripts === null ? '–' : '';
		if (!scripts.available && scripts.scripts.length === 0 && scripts.chosen)
			return t('tools.glance.volumeMissing');
		const parts = [
			scripts.scripts.length
				? many(scripts.scripts.length, 'tools.glance.scriptsOne', 'tools.glance.scriptsMany')
				: t('tools.glance.none'),
		];
		const running = scripts.scripts.filter((s) => s.running).length;
		if (running) parts.push(many(running, 'tools.glance.runningOne', 'tools.glance.runningMany'));
		const next = earliest(scripts.scripts.map((s) => s.next));
		if (next)
			parts.push(
				t('tools.glance.next', {
					time: formatDateTime(new Date(next), scripts.timezone || undefined),
				}),
			);
		return parts.join(' · ');
	});
	const monitoringLine = $derived.by(() => {
		if (!monitoring) return monitoring === null ? '–' : '';
		const settings = monitoring.settings;
		return settings.enabled
			? t('tools.glance.history', { volume: settings.volume, days: settings.days })
			: t('tools.glance.off');
	});
	const disksLine = $derived.by(() => {
		if (!operations) return operations === null ? '–' : '';
		const running = (operations.diskTests ?? []).filter((test) => !test.result).length;
		return running
			? many(running, 'tools.glance.testsOne', 'tools.glance.testsMany')
			: t('tools.glance.noTests');
	});
	const glance = $derived<{ label: MessageKey; value: string; href: string; icon: IconName }[]>([
		{ label: 'ops.tasks', value: tasksLine, href: '/tools/tasks', icon: 'list' },
		{ label: 'ops.maintenance', value: maintenanceLine, href: '/tools/maintenance', icon: 'tool' },
		{ label: 'scripts.title', value: scriptsLine, href: '/tools/scripts', icon: 'terminal' },
		{ label: 'ops.monitoring', value: monitoringLine, href: '/tools/monitoring', icon: 'activity' },
		{ label: 'disktest.title', value: disksLine, href: '/tools/disks', icon: 'disk' },
	]);
</script>

<PageHead title={t('nav.tools')} />

<!-- Desktop: the sidebar lists the tools; the overview only says how they
     stand – one row per tool, the row leads to it. -->
<div class="desk-only">
	<SettingGroup title={t('tools.glance')} id="tools-glance">
		{#each glance as row (row.label)}
			<SettingRow icon={row.icon} title={t(row.label)} href={row.href}>
				{#snippet control()}<span aria-busy={row.value === '' ? 'true' : undefined}
						>{row.value || '…'}</span
					>{/snippet}
			</SettingRow>
		{/each}
	</SettingGroup>
</div>

<!-- Phones: no sidebar; the overview is the grouped list. -->
<div class="groups phone-only">
	{#each toolsGroups as group (group.group)}
		<SettingGroup
			title={t(`tools.group.${group.group}` as MessageKey)}
			id="tools-group-{group.group}"
		>
			{#each group.entries as entry (entry.href)}
				<SettingRow
					icon={entry.icon}
					title={t(entry.label)}
					desc={t(entry.text)}
					href={entry.href}
				/>
			{/each}
		</SettingGroup>
	{/each}
</div>

<style>
	.groups {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	@media (min-width: 761px) {
		.phone-only {
			display: none;
		}
	}
	@media (max-width: 760px) {
		.desk-only {
			display: none;
		}
	}
</style>
