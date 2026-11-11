<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { operationError, type Operations } from '#lib/operations.ts';
	import { taskItems, needsAttention, isActive, type TaskAction } from './tasks.ts';
	import TaskRow from './TaskRow.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	let { compact = false }: { compact?: boolean } = $props();
	let data = $state<Operations | null>(null),
		error = $state(''),
		filter = $state('all'),
		search = $state(''),
		busy = $state(false);
	let inflight = false;
	const tasks = $derived(data ? taskItems(data) : []);
	const problems = $derived(tasks.filter(needsAttention));
	const upcoming = $derived(
		tasks
			.filter((task) => task.state === 'scheduled' || (task.state === 'waiting' && task.at))
			.sort((a, b) => Date.parse(a.at ?? '') - Date.parse(b.at ?? '')),
	);
	const shown = $derived(
		tasks.filter((task) => {
			if (!task.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())) return false;
			if (filter === 'active') return isActive(task);
			if (filter === 'scheduled') return task.state === 'scheduled';
			if (filter === 'done') return task.state === 'done' || task.state === 'cancelled';
			if (filter === 'failed') return needsAttention(task);
			return true;
		}),
	);
	async function load() {
		if (inflight) return;
		inflight = true;
		try {
			data = await api.get<Operations>('/operations');
			error = '';
		} catch (e) {
			error = operationError(e, t);
		} finally {
			inflight = false;
		}
	}
	async function act(action: TaskAction) {
		busy = true;
		error = '';
		try {
			await api.post(action.path, action.body);
			await load();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	onMount(() => {
		return poll(load, { interval: 15000, skip: () => busy });
	});
</script>

<!-- PLAN 78-5: plain headings; every task is a flat row of its own (TaskRow). -->
<SettingGroup title={t('ops.attention')} id="tasks-attention">
	{#snippet tools()}
		<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button>{#if compact}<a
				href="/tools/tasks">{t('ops.openTasks')}</a
			>{/if}
	{/snippet}
	{#if error}<p class="notice-warn" role="alert">{t('ops.stale')} {error}</p>{/if}
	{#each problems.slice(0, compact ? 3 : 50) as task (task.id)}<TaskRow
			{task}
		/>{:else}{#if data && !error}<p class="muted">{t('ops.allClear')}</p>{/if}{/each}
	{#if compact && upcoming.length}<h3>{t('ops.next')}</h3>
		{#each upcoming.slice(0, 3) as task (task.id)}<TaskRow {task} />{/each}{/if}
</SettingGroup>
{#if !compact}
	<SettingGroup title={t('tasks.listTitle')} id="tasks-list">
		<div class="toolbar" aria-label={t('ops.tasks')}>
			{#each ['all', 'active', 'scheduled', 'done', 'failed'] as value (value)}<button
					class="btn"
					class:btn-primary={filter === value}
					aria-pressed={filter === value}
					onclick={() => (filter = value)}>{t(`tasks.filter.${value}` as MessageKey)}</button
				>{/each}
			<input
				class="field"
				aria-label={t('tasks.search')}
				placeholder={t('tasks.search')}
				bind:value={search}
			/>
		</div>
		{#each shown as task (task.id)}<TaskRow {task} {busy} onaction={act} />{:else}<p class="muted">
				{t('ops.none')}
			</p>{/each}
	</SettingGroup>
	<!-- Style W4 (0.74): the figures of the tasks in the status bar. -->
	<StatusBar label={t('tasks.statusBar')}>
		<span>{t('tasks.statCount', { count: tasks.length })}</span>
		<span>{t('tasks.filter.active')}: <b>{tasks.filter(isActive).length}</b></span>
		<span>{t('tasks.filter.scheduled')}: <b>{upcoming.length}</b></span>
		<span class:warn={problems.length > 0}
			>{t('tasks.filter.failed')}: <b>{problems.length}</b></span
		>
	</StatusBar>
{/if}

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
		margin-bottom: var(--space-2);
	}
	a {
		color: var(--accent);
		font-size: var(--text-md);
	}
	.toolbar input {
		flex: 1;
		min-width: 160px;
		max-width: 300px;
	}
	h3 {
		font-size: var(--text-base);
		margin: var(--space-3) 0 var(--space-1);
	}
	p {
		margin: 0;
	}
</style>
