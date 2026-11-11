<script lang="ts">
	// Settings › KI-Agent › Rechte: the quick choice, the areas with their
	// single tools, and rules for folders ("Ohne Nachfrage / Nachfragen / Verboten").
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import type { FileRoot } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		AREAS,
		areaMode,
		presetOf,
		toolMode,
		type Mode,
		type Preset,
		type Settings,
		type ToolView,
	} from '#lib/agent.ts';

	let {
		settings = $bindable(),
		allAreas = $bindable(),
		open = $bindable(),
		tools,
		roots,
		modeOptions,
	}: {
		settings: Settings;
		/** "weitere Bereiche" was opened. */
		allAreas: boolean;
		/** The areas whose single tools are shown. */
		open: Record<string, boolean>;
		tools: ToolView[];
		roots: FileRoot[];
		modeOptions: { value: Mode; label: string }[];
	} = $props();

	const preset = $derived(settings ? presetOf(settings.policy) : 'standard');
	function setPreset(p: Preset | 'custom') {
		if (!settings || p === 'custom') return;
		settings.policy = { preset: p, folders: settings.policy.folders ?? [] };
	}
	function setArea(area: string, mode: Mode) {
		if (!settings) return;
		settings.policy.areas = { ...(settings.policy.areas ?? {}), [area]: mode };
		// Single tools of the area follow it again.
		const tl = { ...(settings.policy.tools ?? {}) };
		for (const tool of tools.filter((x) => x.area === area)) delete tl[tool.name];
		settings.policy.tools = tl;
	}
	function setTool(name: string, mode: Mode) {
		if (!settings) return;
		settings.policy.tools = { ...(settings.policy.tools ?? {}), [name]: mode };
	}
	const areaLabel = (a: string) => t(`agent.area.${a}` as MessageKey);
	// PLAN 68: areas that are rarely changed wait behind "weitere Bereiche"
	// – but only while nothing of them is set apart from the Schnellwahl;
	// a changed area is always in view.
	const RARE = ['backups', 'storage', 'shares', 'accounts', 'plugins', 'home', 'code'];
	const withTools = $derived(AREAS.filter((a) => tools.some((x) => x.area === a)));
	function ownSetting(area: string): boolean {
		if (!settings) return false;
		if (settings.policy.areas?.[area]) return true;
		return tools.some((x) => x.area === area && settings!.policy.tools?.[x.name]);
	}
	const hiddenAreas = $derived(withTools.filter((a) => RARE.includes(a) && !ownSetting(a)));
	const shownAreas = $derived(
		allAreas ? withTools : withTools.filter((a) => !hiddenAreas.includes(a)),
	);
	const kindLabel: Record<string, MessageKey> = {
		read: 'agent.kindRead',
		create: 'agent.kindCreate',
		change: 'agent.kindChange',
	};
</script>

<!-- PLAN 78-1: Schnellwahl, Bereiche and Ordner-Regeln as plain headings
     with rows (they were cards, PLAN 68); an area opens to its tools. -->
<SettingGroup title={t('agent.preset')}>
	<SettingRow icon="zap" title={t('agent.presetRow')}>
		{#snippet control()}
			<div class="preset-tools">
				<Segments
					compact
					label={t('agent.preset')}
					value={preset}
					options={[
						{ value: 'read', label: t('agent.presetRead') },
						{ value: 'standard', label: t('agent.presetStandard') },
						{ value: 'all', label: t('agent.presetAll') },
						{ value: 'custom', label: t('agent.presetCustom') },
					]}
					onchange={(v) => setPreset(v as Preset | 'custom')}
				/>
			</div>
		{/snippet}
	</SettingRow>
	<SettingRow icon="lock" title={t('agent.alwaysDeniedLabel')} desc={t('agent.alwaysDenied')} />
</SettingGroup>
<SettingGroup title={t('agent.areas')}>
	{#each shownAreas as area (area)}
		{@const list = tools.filter((x) => x.area === area)}
		{@const m = areaMode(settings.policy, area, list)}
		<SettingRow
			title={areaLabel(area)}
			desc={t('agent.toolCount', { count: list.length }) +
				(m === 'mixed' ? ' · ' + t('agent.mixed') : '')}
			bind:open={() => !!open[area], (on) => (open[area] = on)}
		>
			{#snippet control()}
				<Segments
					compact
					label={areaLabel(area)}
					value={m === 'mixed' ? ('' as Mode) : m}
					options={modeOptions}
					onchange={(v) => setArea(area, v)}
				/>
			{/snippet}
			{#each list as tool (tool.name)}
				<div class="srow-sub">
					<span class="srow-text tname"
						><code>{tool.name}</code>
						<span class="chip {tool.dangerous ? 'warn' : ''}"
							>{t(kindLabel[tool.kind] ?? 'agent.kindChange')}</span
						></span
					>
					<div class="srow-ctl">
						<Segments
							compact
							label={tool.name}
							value={toolMode(settings.policy, tool)}
							options={modeOptions}
							onchange={(v) => setTool(tool.name, v)}
						/>
					</div>
				</div>
			{/each}
		</SettingRow>
	{/each}
	{#if hiddenAreas.length}
		<div class="row-actions">
			<button type="button" class="link-btn" onclick={() => (allAreas = !allAreas)}
				>{allAreas
					? t('agent.areasFewer')
					: t('agent.areasMore', { count: hiddenAreas.length })}</button
			>
		</div>
	{/if}
</SettingGroup>
<SettingGroup title={t('agent.folderRules')}>
	{#snippet tools()}
		<button
			type="button"
			class="btn"
			onclick={() =>
				(settings!.policy.folders = [
					...(settings!.policy.folders ?? []),
					{ root: roots[0]?.id ?? '', path: '', mode: 'deny' },
				])}><Icon name="plus" size={15} />{t('agent.folderRule')}</button
		>
	{/snippet}
	{#each settings.policy.folders ?? [] as f, i (i)}
		<!-- A rule is a row without a name: the folder, then what applies. -->
		<div class="srow plain">
			<div class="srow-head frule">
				<span class="frule-where">
					<select class="field" bind:value={f.root} aria-label={t('agent.locationRoot')}>
						{#each roots as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
					</select>
					<input
						class="field"
						bind:value={f.path}
						placeholder={t('agent.locationPath')}
						aria-label={t('agent.locationPath')}
					/>
				</span>
				<div class="srow-ctl">
					<Segments
						compact
						label={t('agent.folderRules')}
						bind:value={f.mode}
						options={modeOptions}
					/>
					<button
						type="button"
						class="btn"
						aria-label={t('common.delete')}
						title={t('common.delete')}
						onclick={() => settings!.policy.folders!.splice(i, 1)}
						><Icon name="trash" size={15} /></button
					>
				</div>
			</div>
		</div>
	{:else}
		<p class="muted">{t('agent.folderNone')}</p>
	{/each}
	<p class="muted">{t('agent.folderHint')}</p>
</SettingGroup>

<style>
	.frule-where {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		flex: 1 1 0;
		min-width: 0;
	}
	.frule-where .field {
		flex: 1 1 9rem;
		min-width: 0;
	}
	.frule .srow-ctl {
		max-width: none;
	}
	.link-btn {
		border: 0;
		background: none;
		padding: 0;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.link-btn:hover {
		color: var(--text);
	}
	.link-btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tname {
		flex-direction: row;
		gap: var(--space-2);
		align-items: center;
		flex-wrap: wrap;
	}
	.tname code {
		overflow-wrap: anywhere;
	}
	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.preset-tools {
			width: 100%;
		}
		.preset-tools :global(.segments) {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			width: 100%;
		}
		.frule-where {
			flex-basis: 100%;
		}
	}
</style>
