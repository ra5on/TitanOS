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

<div
	class="task"
	class:problem={task.state === 'failed' || task.state === 'blocked' || task.state === 'unknown'}
>
	<div class="description">
		<a href={task.href}><strong>{displayName}</strong></a>
		<div class="meta">
			{t(`tasks.kind.${task.kind}` as MessageKey)} · {t(
				`tasks.state.${task.state}` as MessageKey,
			)}{#if task.at}
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
	{#if onaction && task.actions?.length}<div class="actions">
			{#each task.actions as action (action.label)}<button
					class="btn"
					disabled={busy}
					onclick={() => onaction?.(action)}
					>{t(`tasks.action.${action.label}` as MessageKey)}</button
				>{/each}
		</div>{/if}
</div>

<style>
	.task {
		display: flex;
		align-items: start;
		justify-content: space-between;
		gap: 16px;
		padding: 14px 0;
		border-bottom: 1px solid var(--border);
		font-size: 0.875rem;
		min-width: 0;
	}
	.description {
		flex: 1;
		min-width: 0;
	}
	.task a {
		color: var(--text);
		text-decoration: none;
	}
	.task a:hover {
		color: var(--accent);
	}
	.meta {
		color: var(--muted);
		font-size: 0.8rem;
		margin-top: 4px;
	}
	.problem strong {
		color: var(--warn-text);
	}
	p {
		margin: 6px 0;
		overflow-wrap: anywhere;
	}
	details {
		margin-top: 6px;
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
	.actions {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	progress {
		margin-top: 8px;
		width: min(100%, 300px);
		accent-color: var(--accent);
	}
	@media (max-width: 760px) {
		.task {
			flex-direction: column;
		}
		.actions {
			width: 100%;
		}
	}
</style>
