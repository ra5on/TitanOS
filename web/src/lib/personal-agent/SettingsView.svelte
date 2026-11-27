<script lang="ts">
	import { onMount, tick } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText } from '#lib/i18n/index.svelte.ts';
	import {
		getMemory,
		saveMemory,
		levels,
		type Settings,
		type State,
		type Job,
		type Source,
		type Level,
	} from './api.ts';
	import { reason, msg } from './labels.ts';
	import Appearance from './Appearance.svelte';
	import Skills from './Skills.svelte';
	import ModelSettings from './ModelSettings.svelte';
	import type { LocalModel } from './model.ts';
	let {
		snapshot,
		settingsFocus,
		model,
		localAllowed,
		onmodelchange,
		sources,
		onjob,
		ondraft,
		onsave,
		pending,
	}: {
		snapshot: State;
		settingsFocus: '' | 'permissions' | 'model';
		model: LocalModel;
		localAllowed: boolean;
		onmodelchange: () => void;
		sources: Source[];
		onjob: (job: Job) => void;
		ondraft: (s: Partial<Settings> | null) => void;
		onsave: (s: Partial<Settings>) => Promise<void>;
		pending: boolean;
	} = $props();
	let memory = $state(''),
		memoryReady = $state(false),
		savingMemory = $state(false),
		error = $state(''),
		saved = $state(false);
	let section = $state<HTMLElement>();
	onMount(() => {
		if (settingsFocus)
			void tick().then(() =>
				section
					?.querySelector<HTMLElement>('[data-section="' + settingsFocus + '"] summary')
					?.focus(),
			);
	});
	const groups = $derived([...new Set(snapshot.capabilities.map((c) => c.app))]);
	onMount(() => {
		let alive = true;
		void getMemory()
			.then((v) => {
				if (alive) {
					memory = (v.items ?? []).join('\n');
					memoryReady = true;
				}
			})
			.catch((e) => {
				if (alive) error = apiErrorText(e);
			});
		return () => {
			alive = false;
		};
	});
	async function keep() {
		savingMemory = true;
		error = '';
		try {
			await saveMemory(
				memory
					.split('\n')
					.map((s) => s.trim())
					.filter(Boolean),
			);
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			savingMemory = false;
		}
	}
	async function choose(id: string, v: Level) {
		try {
			await onsave({ rights: { ...snapshot.settings.rights, [id]: v } });
		} catch {
			/* shown on page */
		}
	}
</script>

<div class="pa-pane" bind:this={section}>
	<h1>{msg('settings')}</h1>
	<div class="pa-settings-card">
		<Appearance settings={snapshot.settings} {ondraft} {onsave} {pending} />
	</div>
	<details
		class="pa-settings-card"
		data-section="permissions"
		open={settingsFocus === 'permissions'}
	>
		<summary><Icon name="shield" />{msg('permissions')}</summary>
		<p class="pa-muted">{msg('rightsNote')}</p>
		{#each groups as group}<h3>{group}</h3>
			<div class="pa-rights">
				{#each snapshot.capabilities.filter((c) => c.app === group) as c (c.id)}
					<label
						><span
							><b>{c.title}</b><small
								>{msg('effective')} · {msg(c.effective)}{reason(c) ? ' · ' + reason(c) : ''}</small
							></span
						>
						<select
							aria-label={c.title}
							value={snapshot.settings.rights?.[c.id] ??
								(c.id === 'agent.example' ? 'auto' : 'ask')}
							disabled={pending}
							onchange={(e) => choose(c.id, e.currentTarget.value as Level)}
						>
							{#each levels as v}<option
									value={v}
									disabled={levels.indexOf(v) > levels.indexOf(c.ceiling)}>{msg(v)}</option
								>{/each}</select
						></label
					>{/each}
			</div>{/each}
	</details>
	<details class="pa-settings-card" data-section="model" open={settingsFocus === 'model'}>
		<summary><Icon name="chip" />{msg('model')}</summary><ModelSettings
			{model}
			{localAllowed}
			{onmodelchange}
		/>
	</details>
	<details class="pa-settings-card">
		<summary><Icon name="bell" />{msg('notices')}</summary>
		{#each ['result', 'blocker', 'approval'] as v}<label class="pa-toggle"
				><input
					type="checkbox"
					checked={(snapshot.settings.notifications ?? {
						result: true,
						blocker: true,
						approval: true,
					})[v as 'result' | 'blocker' | 'approval']}
					disabled={pending}
					onchange={(e) =>
						void onsave({
							notifications: {
								result: true,
								blocker: true,
								approval: true,
								...snapshot.settings.notifications,
								[v]: e.currentTarget.checked,
							},
						}).catch(() => {})}
				/>{msg('notice.' + v)}</label
			>{/each}
	</details>
	<details class="pa-settings-card">
		<summary><Icon name="book" />{msg('memory')}</summary><label class="pa-field"
			>{msg('memoryHint')}<textarea
				rows="4"
				bind:value={memory}
				oninput={() => (saved = false)}
				disabled={!memoryReady}></textarea></label
		>
		<button class="pa-button" disabled={!memoryReady || savingMemory} onclick={keep}
			>{msg('save')}</button
		>{#if saved}<small role="status">{msg('saved')}</small>{/if}
	</details>
	<details class="pa-settings-card">
		<summary><Icon name="grid" />{msg('skills')}</summary><Skills
			caps={snapshot.capabilities}
			{sources}
			{onjob}
		/>
	</details>
	<div class="pa-settings-card">
		<div class="pa-title-row">
			<h3>{msg('backgroundJobs')}</h3>
			<button
				class="pa-icon"
				aria-label={msg(snapshot.settings.paused ? 'continueAll' : 'pauseAll')}
				disabled={pending}
				onclick={() => void onsave({ paused: !snapshot.settings.paused }).catch(() => {})}
				><Icon name={snapshot.settings.paused ? 'play' : 'pause'} /></button
			>
		</div>
		<small>{msg(snapshot.settings.paused ? 'paused' : 'ready')}</small>
	</div>
	{#if error}<p class="pa-warning" role="alert">{error}</p>{/if}
</div>
