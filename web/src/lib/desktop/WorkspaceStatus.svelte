<script lang="ts">
	import Icon from '../components/Icon.svelte';
	import { formatDuration, formatPercent } from '../format.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { noticeLink } from '../notice-link.ts';
	import { isUnread, noticeOwnLink, noticeText, visibleNotices } from '../notifications.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { storage } from '../storage.svelte.ts';
	import { system } from '../system.svelte.ts';
	import type { WorkspaceActivity } from '../task-progress.ts';
	import { appForPath } from './apps.ts';

	let {
		activity,
		onopen,
	}: { activity: WorkspaceActivity | null; onopen: (key: string, href: string) => void } = $props();
	const admin = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	const info = $derived(system.info);
	const issues = $derived.by(() => {
		if (!admin) return [];
		const result = (activity?.backupProblems ?? []).map((job) => ({
			href: noticeLink('backup_failed', job.id)!,
			text: t(job.message, {
				name: job.label,
			}),
		}));
		for (const v of storage.status?.volumes ?? []) {
			if (v.state !== 'ok')
				result.push({
					href: noticeLink('volume_state', v.name)!,
					text: v.name + ' · ' + t(`state.${v.state}`),
				});
		}
		const current = new Set(result.map((item) => item.href));
		for (const notice of visibleNotices()) {
			if (!isUnread(notice) || !['warning', 'error'].includes(notice.severity)) continue;
			const href =
				noticeOwnLink(notice) || noticeLink(notice.kind, notice.subject ?? '') || '/notifications';
			if (!current.has(href)) result.push({ href, text: noticeText(notice).title });
		}
		return result;
	});
	const partial = $derived(activity?.partial || (admin && !storage.status));
	const known = $derived(info && system.reachable && activity?.loaded && !partial);
	const tasks = $derived(activity?.tasks ?? []);

	function follow(event: MouseEvent, href: string) {
		if (event.button || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
		const app = appForPath(new URL(href, location.origin).pathname);
		if (!app) return;
		event.preventDefault();
		onopen('app:' + app.id, href);
	}
</script>

<div class="overview">
	<header>
		<span class="state" class:warn={issues.length > 0} class:healthy={!!known && !issues.length}>
			<Icon name={issues.length ? 'bell' : known ? 'check' : 'activity'} size={19} />
		</span>
		<div>
			<h2>{info?.hostname || session.data?.hostname || t('desktop.widget.status')}</h2>
			<p>
				{issues.length
					? t('desktop.widget.attention')
					: known
						? t('desktop.status.noIssues')
						: t(activity?.loaded ? 'safeguard.unknown' : 'common.loading')}
			</p>
		</div>
	</header>
	{#each issues as issue}
		<a class="problem" href={issue.href} onclick={(event) => follow(event, issue.href)}>
			<span>{issue.text}</span><Icon name="chevronRight" size={15} />
		</a>
	{/each}
	{#each tasks as job (job.id)}
		<a class="task" href={job.href} onclick={(event) => follow(event, job.href)}>
			<span><strong>{t(job.kind)}</strong> {job.title}</span>
			{#if job.stateLabel}<small>{t(job.stateLabel)}</small
				>{:else if job.percent !== undefined}<small>{formatPercent(job.percent)}</small>{:else}<Icon
					name="chevronRight"
					size={15}
				/>{/if}
		</a>
	{:else}
		{#if activity?.loaded && !activity.partial}<p class="quiet">
				<Icon name="activity" size={15} />{t('ui.tasksEmpty')}
			</p>{/if}
	{/each}
	{#if activity?.partial}<small class="partial">{t('ui.tasksPartial')}</small>{/if}
	{#if info}
		<footer>
			<span>{t('desktop.widget.uptime')} <b>{formatDuration(info.uptimeSeconds)}</b></span>
			{#if info.network?.ipv4}<span
					>{info.network.interface} <b>{info.network.ipv4.split('/')[0]}</b></span
				>{/if}
		</footer>
	{/if}
</div>

<style>
	.overview {
		display: grid;
		gap: var(--space-1-5);
		color: var(--text);
	}
	header,
	a,
	.quiet {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	header {
		margin-bottom: var(--space-1);
	}
	h2,
	p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	h2 {
		font-size: var(--text-lg);
		line-height: 1.15;
		letter-spacing: -0.025em;
	}
	header p,
	footer,
	.quiet,
	.partial {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.state {
		display: grid;
		place-items: center;
		border-radius: 12px;
		width: 36px;
		height: 36px;
		flex: none;
		background: var(--surface);
	}
	.healthy {
		color: var(--ok-text);
		background: color-mix(in srgb, var(--ok-text) 13%, var(--surface));
	}
	.warn {
		color: var(--warn-text);
		background: color-mix(in srgb, var(--warn-text) 13%, var(--surface));
	}
	a {
		justify-content: space-between;
		padding: var(--space-1-5) var(--space-2);
		background: var(--surface);
		color: var(--text);
		border-radius: 9px;
		text-decoration: none;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
	}
	.problem {
		border-inline-start: 3px solid var(--warn-text);
	}
	a small {
		flex: none;
		font: inherit;
		font-variant-numeric: tabular-nums;
	}
	a:focus-visible,
	a:hover {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	footer {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
		padding-top: var(--space-1-5);
		border-top: 1px solid var(--nord-line, var(--border));
	}
	footer b {
		color: var(--text);
		font-weight: 550;
		overflow-wrap: anywhere;
	}
</style>
