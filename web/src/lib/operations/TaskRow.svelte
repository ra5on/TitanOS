<script lang="ts">
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { TaskItem, TaskAction } from './tasks.ts';
	let {
		task,
		busy = false,
		onaction,
	}: { task: TaskItem; busy?: boolean; onaction?: (action: TaskAction) => void } = $props();
	const displayName = $derived(task.titleKey ? t(task.titleKey) : task.name);
</script>

<!-- PLAN 78-5: a task is a flat row (the look of SettingRow): name, state
     and details at the left, its buttons at the right. -->
<div
	class="srow plain task"
	class:problem={task.state === 'failed' || task.state === 'blocked' || task.state === 'unknown'}
>
	<div class="srow-head">
		<div class="srow-text">
			<a class="srow-title" href={task.href}><strong>{displayName}</strong></a>
			<div class="srow-desc">
				{t(`tasks.kind.${task.kind}` as MessageKey)} · {t(
					`tasks.state.${task.state}` as MessageKey,
				)}{#if task.stepKey}
					· {t(task.stepKey)}{/if}{#if task.at}
					· <time datetime={task.at}>{new Date(task.at).toLocaleString()}</time>{/if}
			</div>
			{#if task.wait && task.state === 'waiting'}<p>
					{t(`tasks.wait.${task.wait}` as MessageKey)}
				</p>{/if}
			{#if task.kind === 'upload'}<p class="muted">{t('tasks.uploadHint')}</p>{/if}
			{#if task.detail}<details>
					<summary>{t('ops.details')}</summary>
					<pre>{task.detail}</pre>
				</details>{/if}
			{#if task.progress !== undefined && ['running', 'paused', 'waiting'].includes(task.state)}<progress
					max="100"
					value={task.progress}
					aria-label={task.name}
				></progress>{/if}
		</div>
		{#if onaction && task.actions?.length}<div class="srow-ctl">
				{#each task.actions as action (action.label)}<button
						class="btn"
						disabled={busy}
						onclick={() => onaction?.(action)}
						>{t(`tasks.action.${action.label}` as MessageKey)}</button
					>{/each}
			</div>{/if}
	</div>
</div>

<style>
	.task {
		font-size: var(--text-md);
	}
	.task a {
		color: var(--text);
		text-decoration: none;
	}
	.task a:hover {
		color: var(--accent);
	}
	.problem strong {
		color: var(--warn-text);
	}
	p {
		margin: var(--space-1-5) 0;
		overflow-wrap: anywhere;
	}
	details {
		margin-top: var(--space-1-5);
	}
	summary {
		cursor: pointer;
		color: var(--muted);
	}
	pre {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 200px;
		overflow: auto;
		font: inherit;
	}
	progress {
		margin-top: var(--space-2);
		width: min(100%, 300px);
		accent-color: var(--accent);
	}
</style>
