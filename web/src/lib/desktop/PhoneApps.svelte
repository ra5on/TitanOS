<script lang="ts">
	// §121 (owner 28.11.2026, draft Entwuerfe/121-handy-start/B-apps-liste): the
	// tab "Apps" of the phone – every program the account may open as a row
	// with a line on what it is doing (running, updates, volumes), filters
	// and the search of the menu. Names wrap only between whole words.
	import Icon from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { formatPercent } from '#lib/format.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { programs, type Program } from './programs.svelte.ts';
	import { sources } from './sources.svelte.ts';
	import { openPhoneProgram } from './phone-open.ts';

	let { onsearch }: { onsearch: () => void } = $props();
	type Filter = 'all' | 'system' | 'docker' | 'vm';
	const FILTERS: [Filter, MessageKey][] = [
		['all', 'phoneapps.filter.all'],
		['system', 'phoneapps.filter.system'],
		['docker', 'phoneapps.filter.docker'],
		['vm', 'phoneapps.filter.vm'],
	];
	let filter = $state<Filter>('all');
	const all = $derived(programs());
	const groups = $derived(
		(
			[
				['docker', 'phoneapps.filter.docker'],
				['system', 'phoneapps.filter.system'],
				['webapp', 'phoneapps.webapps'],
				['vm', 'phoneapps.filter.vm'],
			] as [Program['kind'], MessageKey][]
		)
			.filter(([kind]) => filter === 'all' || filter === kind)
			.map(([kind, label]) => ({
				kind,
				label,
				items: all.filter((p) => p.kind === kind).sort((a, b) => a.label.localeCompare(b.label)),
			}))
			.filter((g) => g.items.length),
	);
	const updates = (g: { items: Program[] }) =>
		g.items.reduce((sum, p) => sum + (p.badge?.count ?? 0), 0);

	function status(p: Program): string {
		if (p.key === 'app:storage') {
			const volumes = storage.status?.volumes ?? [];
			if (!volumes.length) return '';
			const bad = volumes.filter((v) => v.state !== 'ok').length;
			if (bad) return p.badge?.title ?? '';
			const used = volumes.reduce((s, v) => s + (v.usedBytes || 0), 0);
			const size = volumes.reduce((s, v) => s + (v.sizeBytes || 0), 0);
			return t('phoneapps.status.storage', {
				count: volumes.length,
				used: size ? formatPercent((used / size) * 100) : '–',
			});
		}
		if (p.key === 'app:docker' && sources.containers.length) {
			const running = sources.containers.filter((c) => c.state === 'running').length;
			const text = t('phoneapps.status.containers', { running, total: sources.containers.length });
			return p.badge?.count ? `${text} · ${p.badge.title}` : text;
		}
		return p.badge?.title ?? '';
	}
</script>

<main class="apps-page" data-phone-apps>
	<h1>{t('phone.tabs.apps')}</h1>
	<button type="button" class="search" onclick={onsearch}
		><Icon name="search" size={18} /><span>{t('phoneapps.search')}</span></button
	>
	<div class="chips" role="group" aria-label={t('phoneapps.filter.label')}>
		{#each FILTERS as [id, label] (id)}
			<button type="button" aria-pressed={filter === id} onclick={() => (filter = id)}
				>{t(label)}</button
			>
		{/each}
	</div>
	{#each groups as group (group.kind)}
		<div class="group-head">
			<h2>{t(group.label)}</h2>
			{#if updates(group)}<span class="pill">{t('start.updates', { count: updates(group) })}</span
				>{/if}
		</div>
		<ul class="rows">
			{#each group.items as p (p.key)}
				{@const line = status(p)}
				<li>
					<button
						type="button"
						data-home-key={p.key}
						onclick={(e) => openPhoneProgram(p, e.currentTarget.querySelector('.symbol'))}
					>
						<span class="symbol"
							><DesktopIcon
								glyph={p.glyph}
								gradient={p.gradient}
								image={p.image}
								size={40}
								name=""
							/></span
						>
						<span class="copy"
							><b>{p.label}</b>{#if line}<small
									class:on={p.badge?.dot === 'on'}
									class:warn={p.badge?.warn}>{line}</small
								>{/if}</span
						>
						{#if p.badge?.count}<span class="pill small">{t('docker.update')}</span>{/if}
						<Icon name="chevronRight" size={16} />
					</button>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="empty">{t('phoneapps.none')}</p>
	{/each}
</main>

<style>
	.apps-page {
		box-sizing: border-box;
		flex: 1;
		min-height: 0;
		width: 100%;
		max-width: 680px;
		margin: 0 auto;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: calc(14px + env(safe-area-inset-top)) max(16px, env(safe-area-inset-right))
			calc(var(--phone-dock-height, 0px) + 34px + env(safe-area-inset-bottom))
			max(16px, env(safe-area-inset-left));
		color: #f5f9fb;
	}
	h1 {
		margin: 0;
		font-size: 1.75rem;
		letter-spacing: -0.03em;
		text-shadow: 0 1px 5px #0b1b24;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		min-height: 46px;
		margin-top: 12px;
		padding: 0 14px;
		border-radius: 15px;
		border: 1px solid #c7f0fb40;
		background: var(--nord-chrome);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		color: rgb(245 249 251 / 0.82);
		font: inherit;
		text-align: start;
		cursor: text;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 12px;
	}
	.chips button,
	.pill {
		min-height: 38px;
		padding: 0 14px;
		border-radius: 999px;
		border: 1px solid rgb(224 244 255 / 0.2);
		background: var(--nord-chrome);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		color: inherit;
		font: inherit;
		font-size: 0.88rem;
		font-weight: 600;
		cursor: pointer;
	}
	.chips button[aria-pressed='true'],
	.pill {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent, #0b1622);
	}
	.pill {
		display: inline-flex;
		align-items: center;
		min-height: 32px;
		cursor: default;
		white-space: nowrap;
	}
	.pill.small {
		min-height: 26px;
		padding: 0 10px;
		font-size: 0.76rem;
	}
	.group-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 44px;
		margin-top: 16px;
	}
	h2 {
		margin: 0;
		font-size: 1.08rem;
		text-shadow: 0 1px 5px #0b1b24;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
		border-radius: 18px;
		overflow: hidden;
		border: 1px solid #c7f0fb40;
		background: var(--nord-chrome);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
	}
	.rows li + li {
		border-top: 1px solid #dff6ff2e;
	}
	.rows button {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 56px;
		padding: 8px 12px;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
		-webkit-tap-highlight-color: transparent;
	}
	.symbol {
		flex: none;
		display: block;
	}
	.copy {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-width: 0;
	}
	.copy b {
		font-size: 0.98rem;
		hyphens: manual;
		overflow-wrap: break-word;
	}
	.copy small {
		font-size: 0.8rem;
		opacity: 0.84;
		overflow-wrap: anywhere;
	}
	.copy small.on::before {
		content: '';
		display: inline-block;
		width: 7px;
		height: 7px;
		margin-right: 6px;
		border-radius: 50%;
		background: #3fb950;
		vertical-align: 1px;
	}
	.copy small.warn {
		color: #f2c66d;
		opacity: 1;
	}
	.empty {
		margin-top: 16px;
	}
	/* §121: the large title grows smaller as the list scrolls up (WebKit 26, Chrome). */
	@supports (animation-timeline: scroll()) {
		@media (prefers-reduced-motion: no-preference) {
			h1 {
				animation: title-up linear both;
				animation-timeline: scroll(nearest);
				animation-range: 0 90px;
				transform-origin: left center;
			}
		}
	}
	@keyframes title-up {
		to {
			transform: scale(0.72);
			opacity: 0.6;
		}
	}
	@media (prefers-reduced-transparency: reduce) {
		.search,
		.rows,
		.chips button,
		.pill {
			background: #1a3038;
			backdrop-filter: none;
		}
	}
</style>
