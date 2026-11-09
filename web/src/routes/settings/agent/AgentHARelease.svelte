<script lang="ts">
	// Settings › KI-Agent › Home Assistant: which domains and devices the
	// agent may see and use, per connection (split from +page.svelte, PLAN 75-12).
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { Settings } from '#lib/agent.ts';

	let { settings = $bindable() }: { settings: Settings } = $props();

	// Home Assistant: domains and entities for the agent's own release list.
	let haDomains = $state<string[]>([]);
	let haEntities = $state<{ id: string; name: string; area?: string; domain: string }[] | null>(
		null,
	);
	let haQuery = $state('');
	let haError = $state('');
	// Several Home Assistant connections (PLAN §45): one release list each.
	let haConn = $state('main');
	let haConns = $state<{ id: string; name: string }[]>([]);
	$effect(() => {
		api
			.get<{ connections: { id: string; name: string; active: boolean }[] }>(
				'/homeassistant/connections',
			)
			.then(
				(res) => (haConns = (res.connections ?? []).filter((c) => c.active || c.id === 'main')),
				() => (haConns = []),
			);
	});
	const haRelease = $derived(
		haConn === 'main'
			? settings?.homeassistant.release
			: settings?.homeassistant.releases?.[haConn],
	);
	function chooseHA(id: string) {
		haConn = id;
		haEntities = null;
		haError = '';
	}
	async function loadHA() {
		haError = '';
		const query = haConn === 'main' ? '' : `?connection=${encodeURIComponent(haConn)}`;
		try {
			const s = await api.get<{ domains: string[] }>(`/homeassistant/settings${query}`);
			haDomains = s.domains ?? [];
			haEntities = (
				await api.get<{ entities: { id: string; name: string; area?: string; domain: string }[] }>(
					`/homeassistant/entities${query}`,
				)
			).entities;
		} catch (e) {
			haError = apiErrorText(e);
		}
	}
	function toggleRelease(kind: 'domains' | 'entities', value: string) {
		if (!settings) return;
		const ha = settings.homeassistant;
		const r = haConn === 'main' ? (ha.release ??= {}) : ((ha.releases ??= {})[haConn] ??= {});
		const list = new Set(r[kind] ?? []);
		if (list.has(value)) list.delete(value);
		else list.add(value);
		r[kind] = [...list].sort();
	}
	const haShown = $derived(
		(haEntities ?? [])
			.filter((e) => {
				const q = haQuery.trim().toLowerCase();
				return !q || (e.id + ' ' + e.name + ' ' + (e.area ?? '')).toLowerCase().includes(q);
			})
			.slice(0, 200),
	);
</script>

<section class="tile ui-card" aria-labelledby="ag-ha-release">
	<div class="card-head">
		<h2 id="ag-ha-release">{t('agent.haRelease')}</h2>
		{#if haConns.length > 1}<div class="card-tools">
				<label class="conn"
					><span>{t('notify.haConnection')}</span>
					<select
						class="field"
						value={haConn}
						onchange={(e) => chooseHA((e.currentTarget as HTMLSelectElement).value)}
					>
						{#each haConns as c (c.id)}<option value={c.id}>{c.name || t('ha.conn.first')}</option
							>{/each}
					</select></label
				>
			</div>{/if}
	</div>
	<div class="set-row one"><span class="set-hint">{t('agent.haReleaseHint')}</span></div>
	{#if haEntities === null}
		<div class="set-row">
			<span class="set-label"
				>{t('agent.haReleased', {
					domains: haRelease?.domains?.length ?? 0,
					entities: haRelease?.entities?.length ?? 0,
				})}</span
			>
			<div class="set-control end">
				<button type="button" class="btn" onclick={loadHA}>{t('agent.haLoad')}</button>
			</div>
		</div>
		{#if haError}<div class="set-row one"><p class="notice-warn">{haError}</p></div>{/if}
	{:else}
		<div class="set-row one sub-head"><strong>{t('agent.haDomains')}</strong></div>
		<div class="set-grid domains">
			{#each haDomains as d (d)}
				<label class="set-row"
					><span class="set-label"><code>{d}</code></span><span class="set-control end"
						><input
							type="checkbox"
							class="switch"
							role="switch"
							checked={haRelease?.domains?.includes(d)}
							onchange={() => toggleRelease('domains', d)}
						/></span
					></label
				>
			{/each}
		</div>
		<div class="set-row one">
			<input
				class="field"
				type="search"
				bind:value={haQuery}
				placeholder={t('agent.haSearch')}
				aria-label={t('agent.haSearch')}
			/>
		</div>
		<div class="entities">
			{#each haShown as e (e.id)}
				{@const viaDomain = haRelease?.domains?.includes(e.domain)}
				<label class="set-row"
					><span class="set-label"
						>{e.name}<span class="set-hint"><code>{e.id}</code>{e.area ? ' · ' + e.area : ''}</span
						></span
					><span class="set-control end"
						><input
							type="checkbox"
							class="switch"
							role="switch"
							checked={viaDomain || haRelease?.entities?.includes(e.id)}
							disabled={viaDomain}
							onchange={() => toggleRelease('entities', e.id)}
						/></span
					></label
				>
			{:else}
				<div class="set-row one"><span class="set-hint">{t('agent.haNoEntities')}</span></div>
			{/each}
		</div>
	{/if}
</section>

<style>
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	/* Home Assistant: domains in two columns, the devices in a scroll box. */
	.sub-head {
		padding-top: var(--space-2-5);
		padding-bottom: var(--space-1);
		min-height: 0;
	}
	.domains {
		padding-bottom: var(--space-2);
	}
	.domains > .set-row {
		grid-template-columns: minmax(0, 1fr) auto;
		min-height: 40px;
		padding: var(--space-1-5) 0;
	}
	.entities {
		max-height: 360px;
		overflow-y: auto;
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.entities > .set-row {
		grid-template-columns: minmax(0, 1fr) auto;
	}
	.entities > .set-row:first-child {
		border-top: 0;
	}
	.conn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-base);
	}
	.conn .field {
		width: auto;
		min-width: 9rem;
	}
</style>
