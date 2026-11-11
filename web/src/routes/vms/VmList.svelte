<script lang="ts">
	// The list of VMs beside the chosen one: search, filter, one row per VM
	// with its state; on a phone it folds away behind the VM's name.
	import type { VM } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { nextScheduled } from '#lib/vm-schedule.ts';
	import type { VmsPage } from './vms-page.svelte.ts';

	let { p, current }: { p: VmsPage; current: VM } = $props();
</script>

<aside class="tile" class:open={p.listOpen} aria-labelledby="vms-title">
	<div class="aside-head">
		<h2 class="tile-title" id="vms-title">{t('vms.title')}</h2>
		<button
			type="button"
			class="btn picker"
			aria-expanded={p.listOpen}
			onclick={() => (p.listOpen = !p.listOpen)}
		>
			<span>{vmLabel(current)}</span><Icon name="chevronDown" size={14} />
		</button>
		<button
			type="button"
			class="btn add"
			aria-label={t('vms.adopt.open')}
			title={t('vms.adopt.open')}
			onclick={() => (p.adopting = true)}><Icon name="download" size={16} /></button
		>
		<a class="btn add" href="/vms/new" aria-label={t('vms.create')} title={t('vms.create')}
			><Icon name="plus" size={16} /></a
		>
	</div>
	<div class="list-body">
		<label class="vm-search"
			><span class="sr-only">{t('ui.vmSearch')}</span><input
				class="field"
				type="search"
				bind:value={p.search}
				placeholder={t('ui.vmSearch')}
			/></label
		>
		<div class="filters" role="group" aria-label={t('vms.filter')}>
			{#each ['all', 'running', 'stopped'] as const as entry (entry)}
				<button
					type="button"
					class="filter"
					aria-pressed={p.filter === entry}
					onclick={() => (p.filter = entry)}
					>{t(`vms.filter.${entry}`)} <span class="count">{p.counts[entry]}</span></button
				>
			{/each}
		</div>
		{#if p.shown.length === 0}<p class="vm-empty muted">{t('ui.vmNoResults')}</p>{:else}
			<!-- PLAN 75-19/75-20, style W4: one row per VM, columns instead of cards. -->
			<div class="vm-cols muted" aria-hidden="true">
				<span>{t('vms.col.name')}</span><span>{t('vms.col.state')}</span><span class="num"
					>{t('vms.col.memory')}</span
				>
			</div>
		{/if}
		{#each p.shown as vm (vm.name)}
			{@const selected = vm.name === current.name}
			<div
				class="vm"
				class:selected
				oncontextmenu={(event) => p.rowContext(vm, event)}
				role="presentation"
			>
				<a class="vm-link" href={p.link(vm)} aria-current={selected ? 'page' : undefined}>
					<span class="vm-name">{vmLabel(vm)}</span>
					<span class="vm-mem num">{formatBytes(vm.memoryMiB * 1024 * 1024)}</span>
					<span class="vm-state">
						<span class="dot" class:ok={vm.state === 'running'} aria-hidden="true"></span>{t(
							`vms.state.${vm.state}`,
						)}
						{#if vm.autostart}<span class="autostart" title={t('vms.autostart')}
								><Icon name="power" size={12} /><span class="sr-only">{t('vms.autostart')}</span
								></span
							>{/if}
						{#if nextScheduled(vm.schedule)}
							{@const next = nextScheduled(vm.schedule)!}
							{@const label = t(
								next.action === 'start' ? 'vms.sched.nextStartAt' : 'vms.sched.nextStopAt',
								{ at: formatDateTime(new Date(next.at)) },
							)}
							<span class="autostart" title={label}
								><Icon name="clock" size={12} /><span class="sr-only">{label}</span></span
							>
						{/if}
					</span>
				</a>
				<div class="vm-actions">
					<button
						type="button"
						class="icon"
						aria-label={t('vms.actionsFor', { name: vmLabel(vm) })}
						title={t('ui.more')}
						aria-haspopup="menu"
						onclick={(event) => p.rowButton(vm, event)}><Icon name="more" size={15} /></button
					>
				</div>
			</div>
		{/each}
	</div>
</aside>

<style>
	aside {
		flex: 0 0 260px;
		min-width: 220px;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.aside-head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 50px;
		padding: 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.aside-head h2 {
		flex: 1;
	}
	.picker {
		display: none;
	}
	.add {
		width: 2.25rem;
		height: 2.25rem;
		padding: 0;
		justify-content: center;
	}
	.vm-search {
		display: block;
		margin: var(--space-3) var(--space-3) var(--space-2);
	}
	.filters {
		display: flex;
		gap: var(--space-1);
		padding: 0 var(--space-3) var(--space-2-5);
	}
	.filter {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		height: 1.875rem;
		padding: 0 var(--space-2-5);
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 999px;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.filter[aria-pressed='true'] {
		background: var(--chip);
		border-color: var(--accent);
		color: var(--text);
	}
	.count {
		font-family: var(--font-mono);
	}
	.vm-empty {
		margin: 0;
		padding: var(--space-1) 14px var(--space-3);
		font-size: var(--text-base);
	}
	.vm-cols {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--space-2);
		padding: var(--space-1) calc(2rem + var(--space-3)) var(--space-1) 17px;
		border-bottom: 1px solid var(--panel-border);
		font-size: var(--text-xs);
	}
	.vm-cols span:nth-child(2) {
		display: none;
	}
	.num {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	.vm {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding-right: var(--space-2);
		border-left: 3px solid transparent;
		border-bottom: 1px solid var(--panel-border);
		min-height: 44px;
	}
	.vm:hover {
		background: var(--chip);
	}
	.vm.selected {
		border-left-color: var(--accent);
		background: var(--chip);
	}
	.vm-link {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 2px var(--space-2);
		flex: 1;
		min-width: 0;
		padding: var(--space-2) var(--space-1-5) var(--space-2) 14px;
		color: var(--text);
		text-decoration: none;
	}
	.vm-name {
		font-size: var(--text-md);
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.vm-mem {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.vm-state {
		grid-column: 1 / -1;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-1-5);
		font-size: var(--text-sm);
	}
	.autostart {
		display: inline-flex;
		color: var(--muted);
	}
	.vm-actions {
		display: flex;
		flex: none;
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon:hover:not(:disabled) {
		background: var(--chip);
		color: var(--text);
	}
	.icon:disabled {
		opacity: 0.4;
		cursor: default;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	/* On a phone the list folds away behind the name of the chosen VM. */
	@media (max-width: 760px) {
		aside {
			flex: 1 1 100%;
		}
		.aside-head h2 {
			display: none;
		}
		.picker {
			display: inline-flex;
			flex: 1;
			justify-content: space-between;
			min-width: 0;
		}
		.picker span {
			overflow-wrap: anywhere;
		}
		aside:not(.open) .list-body {
			display: none;
		}
		aside:not(.open) .aside-head {
			border-bottom: 0;
		}
		.vm-actions .icon {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
