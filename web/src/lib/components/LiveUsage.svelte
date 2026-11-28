<script lang="ts">
	// §121: "Live-Nutzung" – four values (CPU, memory, storage, graphics or
	// network) and below them what each app, VM and system container uses.
	// The phone shows it as a sheet, the desktop at the CPU/RAM display of its
	// top bar. A tile chooses what the list is sorted by; a row leads to the
	// page of its app, VM or container (where it can be stopped or restarted).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { live, watchLive, type UsageRow, type UsageSort } from '../live-usage.svelte.ts';
	import { formatBytes, formatMegabytesPerSecond, formatPercent } from '../format.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import Icon from './Icon.svelte';

	let {
		onnavigate,
		onopen,
	}: {
		/** Called before leaving for a page (the sheet closes). */
		onnavigate?: () => void;
		/** On the desktop a page opens in its program's window instead. */
		onopen?: (href: string) => void;
	} = $props();
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let sort = $state<UsageSort>('cpu');
	onMount(() => watchLive(admin));

	const v = $derived(live.value);
	const share = (used: number, total: number) =>
		total > 0 ? Math.min(100, (used / total) * 100) : 0;
	const rows = $derived(
		!v
			? []
			: sort === 'cpu'
				? v.rows
				: [...v.rows].sort((a, b) => b.memoryBytes - a.memoryBytes || b.cpu - a.cpu),
	);
	const peak = $derived(Math.max(1, ...rows.map((r) => (sort === 'cpu' ? r.cpu : r.memoryBytes))));
	function detail(r: UsageRow): string {
		if (r.kind === 'system') return t('live.kind.system');
		if (r.kind === 'vm') return t('live.kind.vm', { n: r.count });
		if (r.kind === 'lxc') return t('live.kind.lxc');
		return t('live.kind.docker', { n: r.count });
	}
	function open(href: string) {
		onnavigate?.();
		if (onopen) onopen(href);
		else void goto(href);
	}
</script>

<div class="live">
	{#if v}
		<div class="tiles" role="radiogroup" aria-label={t('live.sortBy')}>
			<button
				type="button"
				role="radio"
				aria-checked={sort === 'cpu'}
				class="tile"
				onclick={() => (sort = 'cpu')}
			>
				<small>{t('live.cpu')}</small><b>{formatPercent(v.cpu)}</b><small
					>{t('live.threads', { n: v.threads })}</small
				>
				<span class="bar"><i style:width="{v.cpu}%"></i></span>
			</button>
			<button
				type="button"
				role="radio"
				aria-checked={sort === 'memory'}
				class="tile"
				onclick={() => (sort = 'memory')}
			>
				<small>{t('live.memory')}</small><b>{formatBytes(v.memoryUsed)}</b><small
					>{t('live.free', {
						value: formatBytes(Math.max(0, v.memoryTotal - v.memoryUsed)),
					})}</small
				>
				<span class="bar"><i style:width="{share(v.memoryUsed, v.memoryTotal)}%"></i></span>
			</button>
			<a
				class="tile"
				href="/storage"
				onclick={(e) => {
					e.preventDefault();
					open('/storage');
				}}
			>
				<small>{t('live.storage')}</small><b>{formatBytes(v.storageUsed)}</b><small
					>{v.storageTotal
						? t('live.free', { value: formatBytes(Math.max(0, v.storageTotal - v.storageUsed)) })
						: t('live.noVolume')}</small
				>
				<span class="bar"><i style:width="{share(v.storageUsed, v.storageTotal)}%"></i></span>
			</a>
			{#if v.gpu}
				<div class="tile">
					<small>{t('live.gpu')}</small><b>{formatPercent(v.gpu.percent)}</b><small
						>{v.gpu.memoryUsed !== null
							? t('live.gpuMemory', { value: formatBytes(v.gpu.memoryUsed) })
							: ''}</small
					>
					<span class="bar"><i style:width="{v.gpu.percent}%"></i></span>
				</div>
			{:else}
				<div class="tile">
					<small>{t('live.network')}</small><b
						>{t('live.rate', { value: formatMegabytesPerSecond(v.rx + v.tx) })}</b
					><small
						>{t('live.rates', {
							rx: formatMegabytesPerSecond(v.rx),
							tx: formatMegabytesPerSecond(v.tx),
						})}</small
					>
				</div>
			{/if}
		</div>
		{#if admin && rows.length}
			<h3>{t(sort === 'cpu' ? 'live.byCpu' : 'live.byMemory')}</h3>
			<ul class="rows">
				{#each rows as r (r.id)}
					{@const value = sort === 'cpu' ? r.cpu : r.memoryBytes}
					<li>
						<button type="button" onclick={() => open(r.href)}>
							<span class="name"
								><b>{r.kind === 'system' ? t('live.system') : r.name}</b><small>{detail(r)}</small
								></span
							>
							<span class="bar wide" aria-hidden="true"
								><i class={r.kind} style:width="{Math.max(2, (value / peak) * 100)}%"></i></span
							>
							<b class="value"
								>{sort === 'cpu' ? formatPercent(r.cpu) : formatBytes(r.memoryBytes)}</b
							>
							<Icon name="chevronRight" size={15} />
						</button>
					</li>
				{/each}
			</ul>
			<p class="hint">{t('live.hint')}</p>
		{/if}
	{:else if live.error}
		<p class="hint" role="alert">{t('live.failed')}</p>
	{:else}
		<p class="hint" role="status">{t('live.loading')}</p>
	{/if}
</div>

<style>
	.live {
		display: flex;
		flex-direction: column;
		gap: 12px;
		color: var(--text);
	}
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(118px, 1fr));
		gap: 8px;
	}
	.tile {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		padding: 10px 12px 12px;
		border-radius: 14px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: inherit;
		font: inherit;
		text-align: start;
		text-decoration: none;
		cursor: pointer;
	}
	div.tile {
		cursor: default;
	}
	.tile[aria-checked='true'] {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, var(--surface));
	}
	.tile small {
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.tile b {
		font-size: 1.3rem;
	}
	.bar {
		display: block;
		height: 6px;
		border-radius: 3px;
		background: color-mix(in srgb, var(--text) 13%, transparent);
		overflow: hidden;
		margin-top: 6px;
	}
	.bar i {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
	}
	.bar i.vm {
		background: #b68cf2;
	}
	.bar i.docker {
		background: #f0a35a;
	}
	.bar i.lxc {
		background: #4fc6b4;
	}
	.bar i.system {
		background: var(--muted);
	}
	h3 {
		margin: 6px 2px 0;
		font-size: var(--text-md);
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.rows button {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 52px;
		padding: 8px 12px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--surface);
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.name {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.name b {
		overflow-wrap: anywhere;
	}
	.name small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.bar.wide {
		width: min(30%, 160px);
		flex: none;
		margin: 0;
	}
	.value {
		min-width: 4.5em;
		text-align: end;
		white-space: nowrap;
	}
	.hint {
		margin: 0;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	@media (max-width: 420px) {
		.bar.wide {
			display: none;
		}
	}
</style>
