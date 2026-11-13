<script lang="ts">
	// PLAN 83: a camera's messages about events only while Home Assistant
	// says so (e.g. only while the alarm is armed). Rows of the camera's
	// notification group: a switch, the combination, one row per condition
	// (connection, entity, states). The NAS reads the states from its
	// connection to Home Assistant; an unknown state never holds a message
	// back.
	import { onMount } from 'svelte';
	import { api } from '../api.ts';
	import SettingRow from '../components/settings/SettingRow.svelte';
	import SettingSub from '../components/settings/SettingSub.svelte';
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, t, tOr } from '../i18n/index.svelte.ts';
	import {
		conditionActive,
		conditionOn,
		entityGroups,
		firstStates,
		MAX_RULES,
		newRule,
		stateChoices,
		type HAEntity,
	} from './ha-condition.ts';
	import Pills from './Pills.svelte';
	import type { CameraNotify, HARule } from './types.ts';

	let { notify = $bindable(), id }: { notify: CameraNotify; id: string } = $props();

	type Connection = { id: string; name: string };
	let connections = $state<Connection[] | null>(null);
	let entities = $state<Record<string, HAEntity[]>>({});
	let loadError = $state('');
	let search = $state<Record<number, string>>({});
	let custom = $state<Record<number, string>>({});
	/** Conditions whose entity is being chosen anew. */
	let choosing = $state<Record<number, boolean>>({});
	/** At most this many entities in the list; the search narrows it. */
	const SHOWN = 12;
	const shownEntities = (connection: string, q: string) =>
		entityGroups(listOf(connection), q, '')
			.flatMap(([, list]) => list)
			.slice(0, SHOWN);
	/** Rows of conditions just added stay open while they are filled in. */
	let opened = $state<Record<number, boolean>>({});

	const on = $derived(conditionActive(notify.ha));
	const rules = $derived(notify.ha?.rules ?? []);
	const firstConnection = $derived(connections?.[0]?.id ?? '');

	async function loadEntities(connection: string) {
		if (entities[connection]) return;
		try {
			const q = connection ? `?connection=${encodeURIComponent(connection)}` : '';
			const got = await api.get<{ entities: HAEntity[] }>(`/homeassistant/entities${q}`);
			entities = { ...entities, [connection]: got.entities ?? [] };
		} catch (err) {
			loadError = t('nvr.ha.loadFailed', { error: apiErrorText(err) });
		}
	}
	onMount(async () => {
		try {
			const got = await api.get<{ connections: Connection[] }>('/homeassistant/connection-names');
			connections = got.connections ?? [];
		} catch {
			connections = [];
		}
		const wanted = new Set([firstConnection, ...rules.map((r) => r.connection)]);
		for (const c of wanted) if (connections.length) void loadEntities(c);
	});

	const listOf = (connection: string) => entities[connection] ?? [];
	const entityOf = (rule: HARule) => listOf(rule.connection).find((e) => e.id === rule.entity);

	function setRules(next: HARule[]) {
		notify.ha = { mode: notify.ha?.mode ?? 'all', rules: next };
	}
	function edit(i: number, change: Partial<HARule>) {
		setRules(rules.map((r, j) => (j === i ? { ...r, ...change } : r)));
	}
	function pickEntity(i: number, entity: string) {
		const found = listOf(rules[i].connection).find((e) => e.id === entity);
		edit(i, { entity, states: firstStates(entity, found?.state) });
	}
	async function pickConnection(i: number, connection: string) {
		await loadEntities(connection);
		edit(i, newRule(connection, listOf(connection)));
	}
	function toggleState(i: number, state: string, chosen: boolean) {
		const states = rules[i].states;
		edit(i, { states: chosen ? [...states, state] : states.filter((s) => s !== state) });
	}
	function addCustom(i: number) {
		const value = (custom[i] ?? '').trim();
		if (!value || rules[i].states.includes(value)) return;
		edit(i, { states: [...rules[i].states, value] });
		custom = { ...custom, [i]: '' };
	}
	const stateLabel = (s: string) => tOr(`nvr.ha.state.${s}`, s);
	const summaryOf = (rule: HARule) => {
		const name = entityOf(rule)?.name ?? rule.entity;
		return name ? `${name}: ${rule.states.map(stateLabel).join(', ')}` : t('nvr.ha.pick');
	};
</script>

<SettingRow icon="home" title={t('nvr.ha.title')} for="{id}-ha" id="{id}-ha-row">
	{#snippet description()}
		<p>{t('nvr.ha.hint')}</p>
		<p>{t('nvr.ha.unknownHint')}</p>
		{#if connections && connections.length === 0}<p class="ha-note">{t('nvr.ha.none')}</p>{/if}
	{/snippet}
	{#snippet control()}
		<input
			id="{id}-ha"
			type="checkbox"
			role="switch"
			class="switch"
			checked={on}
			disabled={!on && (!connections || connections.length === 0)}
			onchange={(e) => {
				opened = { 0: e.currentTarget.checked };
				notify.ha = conditionOn(e.currentTarget.checked, firstConnection, listOf(firstConnection));
			}}
		/>
	{/snippet}
</SettingRow>
{#if on}
	{#if loadError}<p class="notice-error" role="alert">{loadError}</p>{/if}
	{#if rules.length > 1}
		<SettingRow icon="list" title={t('nvr.ha.mode')} desc={t('nvr.ha.modeHint')} for="{id}-ha-mode">
			{#snippet control()}
				<select
					id="{id}-ha-mode"
					class="field"
					value={notify.ha?.mode ?? 'all'}
					onchange={(e) =>
						(notify.ha = { mode: e.currentTarget.value === 'any' ? 'any' : 'all', rules })}
				>
					<option value="all">{t('nvr.ha.modeAll')}</option>
					<option value="any">{t('nvr.ha.modeAny')}</option>
				</select>
			{/snippet}
		</SettingRow>
	{/if}
	{#each rules as rule, i (i)}
		{@const current = entityOf(rule)}
		<div class="ha-rule" data-ha-rule={i}>
			<SettingRow
				icon="home"
				title={t('nvr.ha.rule', { n: i + 1 })}
				desc={t('nvr.ha.ruleHint')}
				summary={summaryOf(rule)}
				bind:open={() => opened[i] ?? false, (v) => (opened = { ...opened, [i]: v })}
			>
				{#if connections && connections.length > 1}
					<SettingSub title={t('nvr.ha.connection')} for="{id}-ha-conn-{i}">
						{#snippet control()}
							<select
								id="{id}-ha-conn-{i}"
								class="field"
								value={rule.connection}
								onchange={(e) => void pickConnection(i, e.currentTarget.value)}
							>
								{#each connections as c (c.id)}<option value={c.id}>{c.name || c.id}</option>{/each}
							</select>
						{/snippet}
					</SettingSub>
				{/if}
				<SettingSub title={t('nvr.ha.entity')}>
					{#snippet description()}
						{#if rule.entity}<p class="chosen">
								<b>{current?.name ?? rule.entity}</b>{#if current?.area}
									· {current.area}{/if}
							</p>
							<p class="muted">
								<span class="mono">{rule.entity}</span>{#if current}{' · '}{t('nvr.ha.now', {
										state: stateLabel(current.state),
									})}{/if}
							</p>{/if}
					{/snippet}
					{#snippet control()}
						{#if rule.entity && !choosing[i]}
							<button
								type="button"
								class="btn"
								onclick={() => (choosing = { ...choosing, [i]: true })}>{t('nvr.ha.change')}</button
							>
						{/if}
					{/snippet}
				</SettingSub>
				{#if !rule.entity || choosing[i]}
					<div class="srow-block entity-pick">
						<input
							class="field"
							type="search"
							placeholder={t('nvr.ha.search')}
							aria-label={t('nvr.ha.search')}
							value={search[i] ?? ''}
							oninput={(e) => (search = { ...search, [i]: e.currentTarget.value })}
						/>
						{#if shownEntities(rule.connection, search[i] ?? '').length === 0}
							<p class="muted">{t('nvr.ha.noMatch')}</p>
						{:else}
							<ul class="entities" aria-label={t('nvr.ha.entity')}>
								{#each shownEntities(rule.connection, search[i] ?? '') as e (e.id)}
									<li>
										<button
											type="button"
											class="entity"
											aria-pressed={e.id === rule.entity}
											onclick={() => {
												pickEntity(i, e.id);
												choosing = { ...choosing, [i]: false };
											}}
											><b>{e.name}</b><span class="muted">{e.area ? `${e.area} · ` : ''}{e.id}</span
											></button
										>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				{/if}
				{#if rule.entity}
					<SettingSub title={t('nvr.ha.states')}>
						{#snippet description()}
							{#if rule.states.length === 1}<p class="muted">{t('nvr.ha.lastState')}</p>{/if}
						{/snippet}
						{#snippet control()}
							<Pills
								label={t('nvr.ha.states')}
								options={stateChoices(rule.entity, rule.states, current?.state).map((s) => ({
									value: s,
									label: stateLabel(s),
									disabled: rule.states.length === 1 && rule.states[0] === s,
								}))}
								chosen={rule.states}
								ontoggle={(s, chosen) => toggleState(i, s, chosen)}
							/>
						{/snippet}
					</SettingSub>
					<SettingSub title={t('nvr.ha.custom')} for="{id}-ha-custom-{i}">
						{#snippet control()}
							<span class="custom">
								<input
									id="{id}-ha-custom-{i}"
									class="field"
									maxlength="255"
									value={custom[i] ?? ''}
									oninput={(e) => (custom = { ...custom, [i]: e.currentTarget.value })}
									onkeydown={(e) => {
										if (e.key === 'Enter') {
											e.preventDefault();
											addCustom(i);
										}
									}}
								/>
								<button type="button" class="btn" onclick={() => addCustom(i)}
									>{t('nvr.ha.customAdd')}</button
								>
							</span>
						{/snippet}
					</SettingSub>
				{/if}
				<div class="srow-block actions">
					<button
						type="button"
						class="btn"
						onclick={() => setRules(rules.filter((_, j) => j !== i))}
						><Icon name="trash" size={14} />{t('nvr.ha.remove', { n: i + 1 })}</button
					>
				</div>
			</SettingRow>
		</div>
	{/each}
	{#if rules.length < MAX_RULES}
		<div class="srow-block actions">
			<button
				type="button"
				class="btn"
				onclick={() => {
					opened = { ...opened, [rules.length]: true };
					setRules([...rules, newRule(firstConnection, [])]);
				}}><Icon name="plus" size={14} />{t('nvr.ha.add')}</button
			>
		</div>
	{/if}
{/if}

<style>
	.ha-note {
		color: var(--warn-text);
	}
	.ha-rule {
		display: contents;
	}
	.entity-pick,
	.custom {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		align-items: center;
		min-width: 0;
	}
	.entity-pick {
		flex-direction: column;
		align-items: stretch;
	}
	.entity-pick .field {
		min-width: 0;
		width: 100%;
	}
	.entities {
		display: grid;
		gap: var(--space-1);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.entity {
		display: grid;
		gap: 0.1rem;
		width: 100%;
		padding: var(--space-2) var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--srow-bg, transparent);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.entity[aria-pressed='true'] {
		border-color: var(--accent);
	}
	.entity span,
	.entity b,
	.chosen {
		overflow-wrap: anywhere;
	}
	.chosen {
		margin: 0;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.actions .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
