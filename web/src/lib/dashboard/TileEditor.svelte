<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { EDITOR_ICONS, type TileOptions } from './options.ts';
	import type { TileId, TilePlacement } from './tiles.ts';
	import { sourceId } from './tiles.ts';
	import { SYSTEM_FIELDS } from './system-fields.ts';
	import { TAILSCALE_FIELDS } from './tailscale.ts';
	import { NETBIRD_FIELDS } from './netbird.ts';
	export type EditPart = 'tile' | 'heading' | 'value' | 'labels';
	let {
		id,
		title,
		settings,
		placement,
		part = 'tile',
		networks = [],
		devices = [],
		items = [],
		defaultItems,
		sensors = [],
		onchange,
		onsize,
		onauto,
		onduplicate,
		onreset,
		ontransfer,
		onclose,
	}: {
		id: TileId;
		title: string;
		settings: TileOptions;
		placement: TilePlacement;
		part: EditPart;
		networks: string[];
		devices: { address: string; name: string }[];
		items: { id: string; name: string }[];
		/** Selection a tile starts with when it stops following all items. */
		defaultItems?: string[];
		sensors?: { id: string; name: string }[];
		onchange: (key: keyof TileOptions, value: unknown) => void;
		onsize: (w: number, h: number, autoHeight?: boolean, pixels?: number) => void;
		onauto: (value: boolean) => void;
		onduplicate: () => void;
		onreset: () => void;
		ontransfer: () => void;
		onclose: () => void;
	} = $props();
	let tab = $state<'content' | 'appearance' | 'size'>('appearance');
	const source = $derived(sourceId(id));
	const metric = $derived(
		['cpu', 'memory', 'igpu', 'npu', 'nvidia', 'accelerators'].includes(source),
	);
	const prefix = $derived(part === 'heading' ? 'title' : part === 'labels' ? 'label' : 'value');
	const sizeKey = $derived((prefix + 'Size') as keyof TileOptions),
		weightKey = $derived((prefix + 'Weight') as keyof TileOptions),
		colorKey = $derived((prefix + 'Color') as keyof TileOptions),
		fontKey = $derived((prefix + 'Font') as keyof TileOptions),
		alignKey = $derived(
			prefix === 'title' ? 'titleAlign' : prefix === 'label' ? 'labelAlign' : 'align',
		);
	const ranges = [
		['padding', 8, 40],
		['radius', 0, 40],
		['opacity', 10, 100],
		['blur', 0, 40],
		['glow', 0, 100],
		['borderWidth', 0, 4],
		['shadow', 0, 40],
		['gap', 2, 30],
	] as const;
	const colorFields = ['background', 'borderColor', 'accent'] as const;
	function reorder(index: number, delta: number) {
		const parts = [...settings.parts];
		[parts[index], parts[index + delta]] = [parts[index + delta], parts[index]];
		onchange('parts', parts);
	}
</script>

<aside class="inspector tile" aria-label={t('dashboard.tileOptions')}>
	<div class="inspector-head">
		<div>
			<span class="eyebrow">{t('dashboard.tileOptions')}</span>
			<h2>{title}</h2>
		</div>
		<button class="icon-button" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={17} /></button
		>
	</div>
	<div class="editor-tabs" role="group" aria-label={t('dashboard.tileOptions')}>
		{#each ['content', 'appearance', 'size'] as value}<button
				class:chosen={tab === value}
				aria-pressed={tab === value}
				onclick={() => (tab = value as typeof tab)}
				>{t(`dashboard.editor.${value}` as MessageKey)}</button
			>{/each}
	</div>
	<div class="editor-body">
		{#if tab === 'appearance'}
			<label
				>{t('dashboard.editor.element')}<select
					class="field"
					value={part}
					onchange={(e) => {
						const choice = e.currentTarget.value;
						part = choice as EditPart;
					}}
					><option value="tile">{t('dashboard.editor.tile')}</option><option value="heading"
						>{t('dashboard.editor.heading')}</option
					><option value="value">{t('dashboard.editor.value')}</option><option value="labels"
						>{t('dashboard.editor.labels')}</option
					></select
				></label
			>
			{#if part !== 'tile'}<fieldset>
					<legend>{t('dashboard.editor.font')}</legend>
					<label
						>{t('dashboard.editor.fontSize')}<output>{settings[sizeKey]} px</output><input
							type="range"
							aria-label={t('dashboard.editor.fontSize')}
							min="11"
							max={prefix === 'value' ? 96 : prefix === 'title' ? 36 : 28}
							value={Number(settings[sizeKey])}
							oninput={(e) => onchange(sizeKey, Number(e.currentTarget.value))}
						/></label
					>
					<div class="pair">
						<label
							>{t('dashboard.editor.weight')}<select
								class="field"
								value={String(settings[weightKey])}
								onchange={(e) => onchange(weightKey, Number(e.currentTarget.value))}
								>{#each [400, 500, 600] as weight}<option value={weight}>{weight}</option
									>{/each}</select
							></label
						><label
							>{t('dashboard.editor.font')}<select
								class="field"
								value={String(settings[fontKey])}
								onchange={(e) => onchange(fontKey, e.currentTarget.value)}
								><option value="sans">IBM Plex Sans</option><option value="mono"
									>IBM Plex Mono</option
								></select
							></label
						>
					</div>
					<label
						>{t('dashboard.editor.alignment')}<select
							class="field"
							value={settings[alignKey]}
							onchange={(e) => onchange(alignKey, e.currentTarget.value)}
							>{#each ['left', 'center', 'right'] as a}<option value={a}
									>{t(`dashboard.editor.${a}` as MessageKey)}</option
								>{/each}</select
						></label
					>
					<div class="color-row">
						<label
							><input
								type="checkbox"
								checked={!settings[colorKey]}
								onchange={(e) => onchange(colorKey, e.currentTarget.checked ? '' : '#6CB6FF')}
							/>{t('dashboard.editor.themeColor')}</label
						><input
							aria-label={t('dashboard.editor.textColor')}
							type="color"
							value={String(settings[colorKey] || '#6CB6FF')}
							oninput={(e) => onchange(colorKey, e.currentTarget.value)}
						/>
					</div>
				</fieldset>{/if}
			{#if ['docker', 'vms'].includes(source) || /^(app|container|vm):/.test(source)}<fieldset>
					<legend>{t('dashboard.entries')}</legend>
					{#each [['iconSize', 20, 64], ['entryWidth', 120, 320], ['nameLines', 1, 2]] as [key, min, max]}<label
							>{t(`dashboard.${key}` as MessageKey)}<input
								class="field"
								type="number"
								min={Number(min)}
								max={Number(max)}
								value={Number(settings[key as keyof TileOptions])}
								onchange={(e) => onchange(key as keyof TileOptions, Number(e.currentTarget.value))}
							/></label
						>{/each}
				</fieldset>{/if}
			{#if metric}<fieldset>
					<legend>{t('dashboard.editor.visual')}</legend><select
						class="field"
						aria-label={t('dashboard.editor.visual')}
						value={settings.display}
						onchange={(e) => onchange('display', e.currentTarget.value)}
						>{#each ['auto', 'value', 'bar', 'ring', 'history'] as mode}<option value={mode}
								>{t(`dashboard.editor.mode.${mode}` as MessageKey)}</option
							>{/each}</select
					>
				</fieldset>{/if}
			<fieldset>
				<legend>{t('dashboard.editor.surface')}</legend><label
					><input
						type="checkbox"
						checked={settings.surfaceOverride}
						onchange={(e) => onchange('surfaceOverride', e.currentTarget.checked)}
					/>{t('dashboard.visual.override')}</label
				>
				<label
					>{t('dashboard.editor.density')}<select
						class="field"
						value={settings.density}
						onchange={(e) => onchange('density', e.currentTarget.value)}
						>{#each ['compact', 'normal', 'comfortable'] as density}<option value={density}
								>{t(`dashboard.editor.${density}` as MessageKey)}</option
							>{/each}</select
					></label
				>
				{#each ranges as [key, min, max]}<label
						>{t(`dashboard.editor.${key}` as MessageKey)}<output
							>{settings[key]} {['opacity', 'glow'].includes(key) ? '%' : 'px'}</output
						><input
							type="range"
							aria-label={t(`dashboard.editor.${key}` as MessageKey)}
							{min}
							{max}
							value={settings[key]}
							disabled={['opacity', 'blur', 'glow'].includes(key) && !settings.surfaceOverride}
							oninput={(e) => onchange(key, Number(e.currentTarget.value))}
						/></label
					>{/each}
				{#each colorFields as key}<div class="color-row">
						<label
							><input
								type="checkbox"
								checked={!!settings[key]}
								onchange={(e) => onchange(key, e.currentTarget.checked ? '#6CB6FF' : '')}
							/>{t(`dashboard.editor.${key}` as MessageKey)}</label
						><input
							type="color"
							aria-label={t(`dashboard.editor.${key}` as MessageKey)}
							value={settings[key] || '#6CB6FF'}
							oninput={(e) => onchange(key, e.currentTarget.value)}
						/>
					</div>{/each}
			</fieldset>
			<button class="btn" onclick={ontransfer}
				><Icon name="copy" size={14} />{t('dashboard.editor.transfer')}</button
			><button class="link reset" onclick={onreset}>{t('dashboard.editor.reset')}</button>
		{:else if tab === 'content'}
			<label
				>{t('dashboard.editor.customTitle')}<input
					class="field"
					maxlength="80"
					value={settings.title}
					oninput={(e) => onchange('title', e.currentTarget.value)}
				/></label
			>
			<label class="check"
				><input
					type="checkbox"
					checked={settings.showTitle}
					onchange={(e) => onchange('showTitle', e.currentTarget.checked)}
				/>{t('dashboard.editor.showTitle')}</label
			><label class="check"
				><input
					type="checkbox"
					checked={settings.showSummary}
					onchange={(e) => onchange('showSummary', e.currentTarget.checked)}
				/>{t('dashboard.showSummary')}</label
			>
			<label class="check"
				><input
					type="checkbox"
					checked={settings.showIcon}
					onchange={(e) => onchange('showIcon', e.currentTarget.checked)}
				/>{t('dashboard.editor.showIcon')}</label
			>
			{#if settings.showIcon}<label
					>{t('dashboard.editor.icon')}<select
						class="field"
						value={settings.icon}
						onchange={(e) => onchange('icon', e.currentTarget.value)}
						>{#each EDITOR_ICONS as icon}<option value={icon}
								>{t(`dashboard.editor.icons.${icon}` as MessageKey)}</option
							>{/each}</select
					></label
				>{/if}
			{#if !source.startsWith('vm:') && !source.startsWith('container:') && !['system', 'vms'].includes(source)}<label
					class="check"
					><input
						type="checkbox"
						checked={settings.details}
						onchange={(e) => onchange('details', e.currentTarget.checked)}
					/>{t('dashboard.details')}</label
				>{/if}
			{#if metric}<label class="check"
					><input
						type="checkbox"
						checked={settings.history}
						onchange={(e) => onchange('history', e.currentTarget.checked)}
					/>{t('dashboard.history')}</label
				>{/if}
			{#if source === 'cpu'}<label class="check"
					><input
						type="checkbox"
						checked={settings.temperature}
						onchange={(e) => onchange('temperature', e.currentTarget.checked)}
					/>{t('dashboard.cpuTemperature')}</label
				>{#if settings.temperature}<label
						>{t('dashboard.editor.sensor')}<select
							class="field"
							value={settings.sensor}
							onchange={(e) => onchange('sensor', e.currentTarget.value)}
							><option value="">{t('dashboard.editor.sensorAuto')}</option
							>{#if settings.sensor && !sensors.some((s) => s.id === settings.sensor)}<option
									value={settings.sensor}>{t('temperatures.missing')}</option
								>{/if}{#each sensors as sensor}<option value={sensor.id}>{sensor.name}</option
								>{/each}</select
						></label
					>{/if}{/if}
			{#if source === 'system'}
				<fieldset>
					<legend>{t('dashboard.systemFields')}</legend>
					{#each [...settings.systemFields, ...SYSTEM_FIELDS.filter((f) => !settings.systemFields.includes(f))] as field, index}
						<div class="part-order">
							<label class="check"
								><input
									type="checkbox"
									checked={settings.systemFields.includes(field)}
									onchange={() =>
										onchange(
											'systemFields',
											settings.systemFields.includes(field)
												? settings.systemFields.filter((f) => f !== field)
												: [...settings.systemFields, field],
										)}
								/>{t(`dashboard.field.${field}`)}</label
							>
							{#if settings.systemFields.includes(field)}<button
									class="btn"
									aria-label={`${t(`dashboard.field.${field}`)}: ${t('dashboard.editor.moveUp')}`}
									disabled={index === 0}
									onclick={() => {
										const fields = [...settings.systemFields];
										[fields[index], fields[index - 1]] = [fields[index - 1], fields[index]];
										onchange('systemFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								><button
									class="btn"
									aria-label={`${t(`dashboard.field.${field}`)}: ${t('dashboard.editor.moveDown')}`}
									disabled={index === settings.systemFields.length - 1}
									onclick={() => {
										const fields = [...settings.systemFields];
										[fields[index], fields[index + 1]] = [fields[index + 1], fields[index]];
										onchange('systemFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								>{/if}
						</div>
					{/each}
				</fieldset>
				<label
					>{t('dashboard.systemLayout')}<select
						class="field"
						value={settings.systemLayout}
						onchange={(e) => onchange('systemLayout', e.currentTarget.value)}
						><option value="rows">{t('dashboard.systemRows')}</option><option value="stacked"
							>{t('dashboard.systemStacked')}</option
						></select
					></label
				>
				<label class="check"
					><input
						type="checkbox"
						checked={settings.showLabels}
						onchange={(e) => onchange('showLabels', e.currentTarget.checked)}
					/>{t('dashboard.showLabels')}</label
				>
			{/if}
			{#if source === 'tailscale'}
				<fieldset>
					<legend>{t('tailscale.fields')}</legend>
					{#each [...settings.tailscaleFields, ...TAILSCALE_FIELDS.filter((f) => !settings.tailscaleFields.includes(f))] as field, index (field)}
						<div class="part-order">
							<label class="check"
								><input
									type="checkbox"
									checked={settings.tailscaleFields.includes(field)}
									onchange={() =>
										onchange(
											'tailscaleFields',
											settings.tailscaleFields.includes(field)
												? settings.tailscaleFields.filter((f) => f !== field)
												: [...settings.tailscaleFields, field],
										)}
								/>{t(`tailscale.field.${field}`)}</label
							>
							{#if settings.tailscaleFields.includes(field)}<button
									class="btn"
									aria-label={`${t(`tailscale.field.${field}`)}: ${t('dashboard.editor.moveUp')}`}
									disabled={index === 0}
									onclick={() => {
										const fields = [...settings.tailscaleFields];
										[fields[index], fields[index - 1]] = [fields[index - 1], fields[index]];
										onchange('tailscaleFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								><button
									class="btn"
									aria-label={`${t(`tailscale.field.${field}`)}: ${t('dashboard.editor.moveDown')}`}
									disabled={index === settings.tailscaleFields.length - 1}
									onclick={() => {
										const fields = [...settings.tailscaleFields];
										[fields[index], fields[index + 1]] = [fields[index + 1], fields[index]];
										onchange('tailscaleFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								>{/if}
						</div>
					{/each}
				</fieldset>
				<label class="check"
					><input
						type="checkbox"
						checked={settings.showLabels}
						onchange={(e) => onchange('showLabels', e.currentTarget.checked)}
					/>{t('dashboard.showLabels')}</label
				>
			{/if}
			{#if source === 'netbird'}
				<fieldset>
					<legend>{t('netbird.fields')}</legend>
					{#each [...settings.netbirdFields, ...NETBIRD_FIELDS.filter((f) => !settings.netbirdFields.includes(f))] as field, index (field)}
						<div class="part-order">
							<label class="check"
								><input
									type="checkbox"
									checked={settings.netbirdFields.includes(field)}
									onchange={() =>
										onchange(
											'netbirdFields',
											settings.netbirdFields.includes(field)
												? settings.netbirdFields.filter((f) => f !== field)
												: [...settings.netbirdFields, field],
										)}
								/>{t(`netbird.field.${field}`)}</label
							>
							{#if settings.netbirdFields.includes(field)}<button
									class="btn"
									aria-label={`${t(`netbird.field.${field}`)}: ${t('dashboard.editor.moveUp')}`}
									disabled={index === 0}
									onclick={() => {
										const fields = [...settings.netbirdFields];
										[fields[index], fields[index - 1]] = [fields[index - 1], fields[index]];
										onchange('netbirdFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								><button
									class="btn"
									aria-label={`${t(`netbird.field.${field}`)}: ${t('dashboard.editor.moveDown')}`}
									disabled={index === settings.netbirdFields.length - 1}
									onclick={() => {
										const fields = [...settings.netbirdFields];
										[fields[index], fields[index + 1]] = [fields[index + 1], fields[index]];
										onchange('netbirdFields', fields);
									}}><Icon name="arrowUp" size={13} /></button
								>{/if}
						</div>
					{/each}
				</fieldset>
				<label class="check"
					><input
						type="checkbox"
						checked={settings.showLabels}
						onchange={(e) => onchange('showLabels', e.currentTarget.checked)}
					/>{t('dashboard.showLabels')}</label
				>
			{/if}
			{#if source === 'network'}
				<label
					>{t('dashboard.interface')}<select
						class="field"
						value={settings.interface}
						onchange={(e) => onchange('interface', e.currentTarget.value)}
						><option value="">{t('dashboard.defaultInterface')}</option
						>{#each [...new Set( [...(settings.interface ? [settings.interface] : []), ...networks] )] as name}<option
								>{name}</option
							>{/each}</select
					></label
				>
				{#each ['ipv4', 'ipv6', 'link', 'rates'] as key}<label class="check"
						><input
							type="checkbox"
							checked={Boolean(settings[key as keyof TileOptions])}
							onchange={(e) => onchange(key as keyof TileOptions, e.currentTarget.checked)}
						/>{t(
							(key === 'ipv4' || key === 'ipv6'
								? `network.${key}`
								: `dashboard.${key}`) as MessageKey,
						)}</label
					>{/each}
			{/if}
			{#if ['igpu', 'npu', 'nvidia', 'accelerators'].includes(source)}<label
					>{t('dashboard.editor.device')}<select
						class="field"
						value={settings.device}
						onchange={(e) => onchange('device', e.currentTarget.value)}
						><option value="">{t('storage.all')}</option
						>{#if settings.device && !devices.some((d) => d.address === settings.device)}<option
								value={settings.device}>{settings.device}</option
							>{/if}{#each devices as d}<option value={d.address}>{d.name} · {d.address}</option
							>{/each}</select
					></label
				>
				{#each ['temperature', 'power', 'memory', 'clock', 'driver', 'engines'] as key}<label
						class="check"
						><input
							type="checkbox"
							checked={!settings.hiddenFields.includes(key)}
							onchange={() =>
								onchange(
									'hiddenFields',
									settings.hiddenFields.includes(key)
										? settings.hiddenFields.filter((f) => f !== key)
										: [...settings.hiddenFields, key],
								)}
						/>{t(
							(key === 'driver'
								? 'dashboard.editor.driver'
								: key === 'engines'
									? 'dashboard.editor.engines'
									: `accelerator.${key}`) as MessageKey,
						)}</label
					>{/each}
			{/if}
			{#if ['docker', 'vms', 'shares', 'disks', 'volumes', 'temperatures', 'tailscale', 'netbird'].includes(source)}
				{#if ['docker', 'vms', 'shares', 'volumes', 'disks', 'temperatures', 'tailscale', 'netbird'].includes(source)}<label
						>{t('dashboard.editor.list')}<select
							class="field"
							value={settings.list}
							onchange={(e) => onchange('list', e.currentTarget.value)}
							><option value="list">{t('dashboard.editor.list')}</option><option value="grid"
								>{t('dashboard.editor.grid')}</option
							></select
						></label
					><label
						>{t('dashboard.editor.rows')}<input
							class="field"
							type="number"
							min="1"
							max="100"
							value={settings.rows}
							onchange={(e) => onchange('rows', Number(e.currentTarget.value))}
						/></label
					>{/if}
				{#if source === 'docker' || source === 'vms' || source === 'tailscale' || source === 'netbird'}<label
						class="check"
						><input
							type="checkbox"
							checked={settings.runningOnly}
							onchange={(e) => onchange('runningOnly', e.currentTarget.checked)}
						/>{t(
							source === 'tailscale'
								? 'tailscale.onlineOnly'
								: source === 'netbird'
									? 'netbird.onlineOnly'
									: 'dashboard.editor.runningOnly',
						)}</label
					>{/if}
				{#if items.length || settings.items !== null}<label class="check"
						><input
							type="checkbox"
							checked={settings.items === null}
							onchange={(e) =>
								onchange(
									'items',
									e.currentTarget.checked ? null : (defaultItems ?? items.map((i) => i.id)),
								)}
						/>{t(
							source === 'temperatures'
								? 'dashboard.editor.accountSensors'
								: 'dashboard.editor.allItems',
						)}</label
					>{#if settings.items !== null}{#each [...new Set( [...items.map((i) => i.id), ...settings.items] )] as item}<label
								class="check"
								><input
									type="checkbox"
									checked={settings.items.includes(item)}
									onchange={() =>
										onchange(
											'items',
											settings.items!.includes(item)
												? settings.items!.filter((i) => i !== item)
												: [...settings.items!, item],
										)}
								/>{items.find((i) => i.id === item)?.name ?? item}</label
							>{/each}{/if}{/if}
			{/if}
			{#if ['docker', 'vms'].includes(source) || /^(app|container|vm):/.test(source)}<fieldset>
					<legend>{t('dashboard.entries')}</legend>
					{#each [['iconSize', 20, 64], ['entryWidth', 120, 320], ['nameLines', 1, 2]] as [key, min, max]}<label
							>{t(`dashboard.${key}` as MessageKey)}<input
								class="field"
								type="number"
								min={Number(min)}
								max={Number(max)}
								value={Number(settings[key as keyof TileOptions])}
								onchange={(e) => onchange(key as keyof TileOptions, Number(e.currentTarget.value))}
							/></label
						>{/each}
				</fieldset>{/if}
			{#if metric}<fieldset>
					<legend>{t('dashboard.editor.order')}</legend>{#each settings.parts as key, index}<div
							class="part-order"
						>
							<span>{t(`dashboard.editor.part.${key}` as MessageKey)}</span><button
								class="btn"
								aria-label={t('dashboard.editor.moveUp')}
								disabled={index === 0}
								onclick={() => reorder(index, -1)}><Icon name="arrowUp" size={13} /></button
							><button
								class="btn"
								aria-label={t('dashboard.editor.moveDown')}
								disabled={index === settings.parts.length - 1}
								onclick={() => reorder(index, 1)}><Icon name="arrowUp" size={13} /></button
							>
						</div>{/each}
				</fieldset>{/if}
		{:else}
			<div class="pair">
				<label
					>{t('dashboard.span')}<input
						class="field"
						type="number"
						min="1"
						max="4"
						disabled={placement.locked}
						value={placement.span ?? 1}
						onchange={(e) => onsize(Number(e.currentTarget.value), placement.h)}
					/></label
				><label
					>{t('dashboard.heightPixels')}<input
						class="field"
						type="number"
						min="80"
						max="1600"
						step="8"
						disabled={placement.locked}
						value={placement.height ?? placement.h * 40}
						onchange={(e) =>
							onsize(
								placement.span ?? 1,
								Math.ceil(Number(e.currentTarget.value) / 40),
								false,
								Number(e.currentTarget.value),
							)}
					/></label
				>
			</div>
			<label class="check"
				><input
					type="checkbox"
					disabled={placement.locked}
					checked={placement.autoHeight !== false}
					onchange={(e) => onauto(e.currentTarget.checked)}
				/>{t('dashboard.autoHeight')}</label
			>
			<p class="muted hint">{t('dashboard.editor.keyboardHint')}</p>
			<button class="btn" onclick={onduplicate}
				><Icon name="copy" size={14} />{t('dashboard.editor.duplicate')}</button
			>
		{/if}
	</div>
	<p class="inspector-footer">{t('dashboard.editor.preview')}</p>
</aside>

<style>
	.inspector {
		padding: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		width: 320px;
		flex: none;
		position: sticky;
		top: var(--dashboard-sticky-top, 76px);
		max-height: calc(100vh - var(--dashboard-sticky-top, 76px) - 19px);
		min-width: 0;
	}
	.inspector-head {
		padding: 18px;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.eyebrow {
		font-size: 0.7rem;
		color: var(--muted);
	}
	h2 {
		font-size: 1.1rem;
		font-weight: 500;
		margin-top: 6px;
		overflow-wrap: anywhere;
	}
	.icon-button {
		border: 0;
		background: transparent;
		color: var(--muted);
		cursor: pointer;
	}
	.editor-tabs {
		display: flex;
		border-bottom: 1px solid var(--border);
		padding: 0 12px;
	}
	.editor-tabs button {
		padding: 11px;
		border: 0;
		border-bottom: 2px solid transparent;
		color: var(--muted);
		background: transparent;
		cursor: pointer;
		font-size: 0.8rem;
	}
	.editor-tabs .chosen {
		color: var(--text);
		border-color: var(--accent);
	}
	.editor-body {
		padding: 18px;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 16px;
		scrollbar-gutter: stable;
	}
	label {
		display: block;
		font-size: 0.8rem;
		color: var(--muted);
	}
	label > .field {
		margin-top: 7px;
		width: 100%;
	}
	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
	}
	input[type='range'] {
		display: block;
		width: 100%;
		accent-color: var(--accent);
		margin: 14px 0 4px;
	}
	output {
		float: right;
		color: var(--text);
		font-size: 0.75rem;
		font-family: 'IBM Plex Mono', monospace;
	}
	fieldset {
		margin: 0;
		border: 0;
		border-top: 1px solid var(--border);
		padding: 16px 0 0;
		display: grid;
		gap: 16px;
		min-width: 0;
	}
	legend {
		padding: 0 8px 0 0;
		color: var(--text);
		font-size: 0.85rem;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 8px;
		color: var(--text);
	}
	.color-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.color-row label {
		display: flex;
		align-items: center;
		gap: 7px;
	}
	input[type='color'] {
		width: 32px;
		height: 28px;
		padding: 2px;
		border: 1px solid var(--border);
		background: transparent;
		border-radius: var(--radius-small);
	}
	.reset {
		font-size: 0.8rem;
		color: var(--muted);
	}
	.part-order {
		display: flex;
		gap: 5px;
		align-items: center;
		font-size: 0.8rem;
	}
	.part-order span {
		flex: 1;
	}
	.part-order .btn {
		padding: 5px;
		min-height: 28px;
	}
	.part-order button:last-child {
		transform: rotate(180deg);
	}
	.hint {
		font-size: 0.8rem;
		line-height: 1.6;
	}
	.inspector-footer {
		border-top: 1px solid var(--border);
		padding: 14px 18px;
		color: var(--muted);
		font-size: 0.75rem;
	}
	@media (max-width: 760px) {
		.inspector {
			position: fixed;
			inset: auto 10px 12px;
			width: auto;
			max-height: 75vh;
			z-index: 40;
			box-shadow: 0 10px 40px rgb(0 0 0 / 0.25);
		}
	}
</style>
