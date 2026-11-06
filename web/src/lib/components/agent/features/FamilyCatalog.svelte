<script lang="ts">
	// Skill-Katalog (PLAN 54-G, mockup 12): the signed skills of the
	// community. Before installing, the whole skill is shown (name, when,
	// steps, tools) and the person decides; the server checks the signature
	// again and installs exactly what was shown. Own skills can be exported
	// as a submission package.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { installSkill, loadCatalog, type CatalogSkill } from '#lib/features/family.ts';

	const tk = (key: string, p?: Record<string, string | number>) => t(key as MessageKey, p);

	let skills = $state<CatalogSkill[] | null>(null);
	let own = $state<{ id: string; name: string; builtIn?: boolean }[]>([]);
	let error = $state('');
	let done = $state('');
	let asking = $state<CatalogSkill | null>(null);
	let busy = $state(false);

	async function load() {
		error = '';
		try {
			skills = (await loadCatalog()).skills;
		} catch (e) {
			skills = [];
			error = apiErrorText(e);
		}
		try {
			own = (await api.get<{ skills: typeof own }>('/agent/skills')).skills.filter(
				(s) => !s.builtIn,
			);
		} catch {
			own = [];
		}
	}
	void load();

	async function install(s: CatalogSkill) {
		busy = true;
		error = '';
		try {
			await installSkill(s);
			done = tk('agent.f.family.catalog.installed', { name: s.name });
			asking = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	async function exportSkill(id: string) {
		error = '';
		try {
			await api.download(`/agent/catalog/export/${encodeURIComponent(id)}`, 'skill.json', {
				serverName: true,
			});
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const count = (n?: number) =>
		n ? tk('agent.f.family.catalog.installs', { n: n.toLocaleString() }) : '';
</script>

<div class="cat">
	<p class="muted">{t('agent.f.family.catalog.lead')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if done}<p class="muted" role="status">{done}</p>{/if}
	{#if skills === null}
		<p class="muted">{t('agent.f.family.loading')}</p>
	{:else if skills.length}
		<div class="grp">
			{#each skills as s (s.id)}
				<button
					type="button"
					class="row"
					onclick={() => (asking = s)}
					aria-expanded={asking?.id === s.id}
				>
					<span class="ic" aria-hidden="true">🧩</span>
					<span class="t"
						><strong>{s.name}</strong><small
							>{[s.description, count(s.installs)].filter(Boolean).join(' · ')}</small
						></span
					>
					{#if s.installed}<span class="tag">{t('agent.f.family.catalog.isInstalled')}</span
						>{:else}<span class="tag ok">{t('agent.f.family.catalog.signed')}</span>{/if}
				</button>
			{/each}
		</div>
	{:else if !error}
		<p class="muted">{t('agent.f.family.catalog.empty')}</p>
	{/if}

	{#if asking}
		<div
			class="ask"
			role="region"
			aria-label={tk('agent.f.family.catalog.ask', { name: asking.name })}
		>
			<strong>{tk('agent.f.family.catalog.ask', { name: asking.name })}</strong>
			<small
				>{t('agent.f.family.catalog.uses')}: {asking.tools.length
					? asking.tools.join(', ')
					: t('agent.f.family.catalog.noTools')}</small
			>
			{#if asking.author}<small class="muted"
					>{tk('agent.f.family.catalog.by', { who: asking.author })}</small
				>{/if}
			<small class="muted">{t('agent.f.family.catalog.when')}: {asking.when}</small>
			<pre class="steps">{asking.steps}</pre>
			{#if asking.unknown.length}
				<p class="notice-error" role="alert">
					{tk('agent.f.family.catalog.unknown', { tools: asking.unknown.join(', ') })}
				</p>
			{/if}
			{#if asking.secret}<p class="notice-error" role="alert">
					{t('agent.f.family.catalog.secret')}
				</p>{/if}
			<small class="muted">{t('agent.f.family.catalog.askHint')}</small>
			<div class="btns">
				<button
					type="button"
					class="btn btn-primary"
					disabled={busy || asking.unknown.length > 0 || !!asking.secret}
					onclick={() => install(asking!)}
					>{asking.installed
						? t('agent.f.family.catalog.update')
						: t('agent.f.family.catalog.install')}</button
				>
				<button type="button" class="btn" onclick={() => (asking = null)}
					>{t('agent.f.family.cancel')}</button
				>
			</div>
		</div>
	{/if}

	<h4 class="h">{t('agent.f.family.catalog.share')}</h4>
	<p class="muted small">{t('agent.f.family.catalog.shareHint')}</p>
	{#if own.length}
		<div class="grp">
			{#each own as s (s.id)}
				<div class="row">
					<span class="t"><strong>{s.name}</strong></span>
					<button type="button" class="btn" onclick={() => exportSkill(s.id)}
						>{t('agent.f.family.catalog.export')}</button
					>
				</div>
			{/each}
		</div>
	{:else}
		<p class="muted small">{t('agent.f.family.catalog.noOwn')}</p>
	{/if}
</div>

<style>
	.cat {
		display: flex;
		flex-direction: column;
		gap: 10px;
		min-width: 0;
	}
	.cat p {
		margin: 0;
	}
	.h {
		margin: 8px 2px -2px;
		font-size: var(--text-xs, 0.75rem);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.grp {
		border: 1px solid var(--border);
		border-radius: 14px;
		overflow: hidden;
		background: color-mix(in srgb, var(--surface) 55%, transparent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 10px 12px;
		border: 0;
		border-top: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		min-width: 0;
	}
	button.row {
		cursor: pointer;
	}
	.row:first-child {
		border-top: 0;
	}
	.ic {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: 9px;
		background: color-mix(in srgb, #a855f7 18%, transparent);
	}
	.t {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.t small {
		color: var(--muted);
		line-height: 1.35;
	}
	.tag {
		flex-shrink: 0;
		font-size: var(--text-xs, 0.75rem);
		padding: 2px 9px;
		border-radius: 999px;
		border: 1px solid currentColor;
		color: var(--muted);
	}
	.tag.ok {
		color: var(--success, #22c55e);
	}
	.ask {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 12px 14px;
		border-radius: 14px;
		border: 1px solid var(--warning, #eab308);
		background: color-mix(in srgb, var(--warning, #eab308) 10%, transparent);
		min-width: 0;
	}
	.steps {
		margin: 0;
		max-height: 14rem;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font: inherit;
		font-size: var(--text-sm, 0.85rem);
		padding: 8px 10px;
		border-radius: 8px;
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.btns {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 4px;
	}
	.small {
		font-size: var(--text-sm, 0.85rem);
	}
</style>
