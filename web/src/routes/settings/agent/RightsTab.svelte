<script lang="ts">
	// Settings › KI-Agent › Rechte: the quick choice, the areas with their
	// single tools, and rules for folders ("Ohne Nachfrage / Nachfragen / Verboten").
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
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

<!-- PLAN 68: Schnellwahl, Bereiche and Ordner-Regeln as cards with rows. -->
<section class="tile ui-card" aria-labelledby="ag-perm">
	<div class="card-head">
		<h2 id="ag-perm">{t('agent.preset')}</h2>
		<span class="card-meta">{t('agent.presetShort')}</span>
		<div class="card-tools preset-tools">
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
	</div>
	<div class="set-row one">
		<span class="set-label"
			>{t('agent.alwaysDeniedLabel')}<span class="set-hint">{t('agent.alwaysDenied')}</span></span
		>
	</div>
</section>
<section class="tile ui-card" aria-labelledby="ag-areas">
	<div class="card-head">
		<h2 id="ag-areas">{t('agent.areas')}</h2>
	</div>
	{#each shownAreas as area (area)}
		{@const list = tools.filter((x) => x.area === area)}
		{@const m = areaMode(settings.policy, area, list)}
		<div class="set-row area">
			<button
				type="button"
				class="set-label expand"
				aria-expanded={!!open[area]}
				onclick={() => (open[area] = !open[area])}
			>
				<Icon name={open[area] ? 'chevronDown' : 'chevronRight'} size={15} />
				<span
					><strong>{areaLabel(area)}</strong><span class="set-hint"
						>{t('agent.toolCount', { count: list.length })}{m === 'mixed'
							? ' · ' + t('agent.mixed')
							: ''}</span
					></span
				>
			</button>
			<div class="set-control end">
				<Segments
					compact
					label={areaLabel(area)}
					value={m === 'mixed' ? ('' as Mode) : m}
					options={modeOptions}
					onchange={(v) => setArea(area, v)}
				/>
			</div>
		</div>
		{#if open[area]}
			{#each list as tool (tool.name)}
				<div class="set-row area tool">
					<span class="set-label tname"
						><code>{tool.name}</code>
						<span class="chip {tool.dangerous ? 'warn' : ''}"
							>{t(kindLabel[tool.kind] ?? 'agent.kindChange')}</span
						></span
					>
					<div class="set-control end">
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
		{/if}
	{/each}
	{#if hiddenAreas.length}
		<div class="card-foot">
			<button type="button" class="link-btn" onclick={() => (allAreas = !allAreas)}
				>{allAreas
					? t('agent.areasFewer')
					: t('agent.areasMore', { count: hiddenAreas.length })}</button
			>
		</div>
	{/if}
</section>
<section class="tile ui-card" aria-labelledby="ag-folders">
	<div class="card-head">
		<h2 id="ag-folders">{t('agent.folderRules')}</h2>
		<div class="card-tools">
			<button
				type="button"
				class="btn"
				onclick={() =>
					(settings!.policy.folders = [
						...(settings!.policy.folders ?? []),
						{ root: roots[0]?.id ?? '', path: '', mode: 'deny' },
					])}><Icon name="plus" size={15} />{t('agent.folderRule')}</button
			>
		</div>
	</div>
	{#each settings.policy.folders ?? [] as f, i (i)}
		<div class="set-row frule">
			<span class="set-label frule-where">
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
			<div class="set-control end">
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
	{:else}
		<div class="set-row one"><span class="set-hint">{t('agent.folderNone')}</span></div>
	{/each}
	<div class="card-foot"><span class="grow">{t('agent.folderHint')}</span></div>
</section>

<style>
	/* PLAN 68: every tab in cards with rows "label (+ hint) left, control
	   right"; lists as rows; the card's own actions in its foot. */
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}

	/* The hint on the card's surface: a little toward the text for 4.5:1. */
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}

	.set-control .btn {
		max-width: 100%;
		white-space: normal;
		height: auto;
	}

	/* The ways to the NAS: a radio list as rows. */

	.card-foot > .grow {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}

	/* PLAN 68: an area is a row; the label opens its tools. */
	.area .expand {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
		text-align: left;
		padding: 0;
	}
	.area .expand > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.area .expand strong {
		font-weight: 600;
	}
	.area .expand:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.area .set-control :global(.segments),
	.frule .set-control :global(.segments) {
		flex-wrap: nowrap;
	}
	.area {
		padding-top: 9px;
		padding-bottom: 9px;
	}
	.area.tool {
		padding-left: 40px;
		background: color-mix(in srgb, var(--text) 2.5%, transparent);
	}
	.frule-where {
		flex-direction: row;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.frule-where .field {
		flex: 1 1 9rem;
		min-width: 0;
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
	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.preset-tools {
			width: 100%;
			margin-left: 0;
		}
		.preset-tools :global(.segments) {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			width: 100%;
		}
		.area,
		.frule {
			grid-template-columns: minmax(0, 1fr) !important;
		}
		.area .set-control,
		.frule .set-control {
			justify-content: flex-start;
		}
		.area.tool {
			padding-left: var(--space-4);
		}
	}
	.tname {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		flex-wrap: wrap;
	}
</style>
