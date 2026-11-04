<script lang="ts">
	import { api } from '#lib/api.ts';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
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
	const glance = $derived<{ label: MessageKey; value: string }[]>([
		{ label: 'ops.tasks', value: tasksLine },
		{ label: 'ops.maintenance', value: maintenanceLine },
		{ label: 'scripts.title', value: scriptsLine },
		{ label: 'ops.monitoring', value: monitoringLine },
		{ label: 'disktest.title', value: disksLine },
	]);
</script>

<PageHead title={t('nav.tools')} />

<!-- Desktop: the sidebar lists the tools; the overview only says how they stand. -->
<section class="tile panel desk-only" aria-labelledby="tools-glance">
	<h2 class="tile-title" id="tools-glance">{t('tools.glance')}</h2>
	<dl class="facts">
		{#each glance as row (row.label)}
			<dt>{t(row.label)}</dt>
			<dd aria-busy={row.value === '' ? 'true' : undefined}>{row.value || '…'}</dd>
		{/each}
	</dl>
</section>

<!-- Phones: no sidebar; the overview is the grouped list. -->
<div class="groups phone-only">
	{#each toolsGroups as group (group.group)}
		<section class="tile tools-group" aria-labelledby="tools-group-{group.group}">
			<h2 class="tile-title" id="tools-group-{group.group}">
				{t(`tools.group.${group.group}` as MessageKey)}
			</h2>
			<ul>
				{#each group.entries as entry (entry.href)}
					<li>
						<a href={entry.href}
							><span class="tool-icon"><Icon name={entry.icon} size={16} /></span><span class="what"
								><span class="name">{t(entry.label)}</span><span class="muted text"
									>{t(entry.text)}</span
								></span
							><span class="chevron"><Icon name="arrowRight" size={14} /></span></a
						>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
</div>

<style>
	.facts {
		display: grid;
		grid-template-columns: 12rem minmax(0, 1fr);
		gap: var(--space-2) var(--space-4);
		margin: 0;
		font-size: var(--text-md);
	}
	.facts dt {
		color: var(--muted);
	}
	.facts dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.groups {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	.tools-group {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow: hidden;
	}
	.tools-group h2 {
		margin: 0;
		padding: 10px var(--tile-pad);
		border-bottom: 1px solid var(--panel-border);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li + li {
		border-top: 1px solid var(--panel-border);
	}
	a {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 44px;
		padding: var(--space-2) var(--tile-pad);
		color: var(--text);
		text-decoration: none;
	}
	a:hover {
		background: var(--chip);
	}
	a:focus-visible {
		outline-offset: -2px;
	}
	.tool-icon {
		display: grid;
		place-items: center;
		flex: none;
		width: 28px;
		height: 28px;
		border-radius: var(--radius-small);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		color: var(--accent);
	}
	.what {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.name {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.text {
		font-size: var(--text-sm);
	}
	.chevron {
		display: flex;
		flex: none;
		color: var(--muted);
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
