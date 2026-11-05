<script lang="ts">
	// What the KI-Agent learned and plans (PLAN 44, "Lernen"): the memory
	// (facts it remembered, changeable and removable), the skills (built-in
	// ones can be switched off, own ones changed) and the recurring tasks.
	// Entries written while the chat had read outside content carry a mark.
	import Dialog from '#lib/components/Dialog.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Memory, Project, Schedule, Skill } from '#lib/agent.ts';

	let {
		tab: startTab = 'memory',
		projects = [],
		onopenchat,
		onclose,
	}: {
		tab?: 'memory' | 'skills' | 'schedules';
		projects?: Project[];
		onopenchat: (id: string) => void;
		onclose: () => void;
	} = $props();

	let tab = $state((() => startTab)());
	let memory = $state<Memory[]>([]);
	let skills = $state<Skill[]>([]);
	let schedules = $state<Schedule[]>([]);
	let error = $state('');
	let editing = $state<{ kind: 'memory' | 'skill' | 'schedule'; id: string } | null>(null);
	let draft = $state<Record<string, string>>({});

	async function load() {
		error = '';
		try {
			[memory, skills, schedules] = await Promise.all([
				api.get<{ memory: Memory[] }>('/agent/memory').then((r) => r.memory),
				api.get<{ skills: Skill[] }>('/agent/skills').then((r) => r.skills),
				api.get<{ schedules: Schedule[] }>('/agent/schedules').then((r) => r.schedules),
			]);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
	});

	async function run(fn: () => Promise<unknown>) {
		error = '';
		try {
			await fn();
			editing = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const projectName = (id?: string) => projects.find((p) => p.id === id)?.name ?? '';
	const everyText: Record<string, MessageKey> = {
		hourly: 'agent.everyHourly',
		daily: 'agent.everyDaily',
		weekly: 'agent.everyWeekly',
		monthly: 'agent.everyMonthly',
		trigger: 'agent.everyTrigger',
	};
	const weekdays = $derived(t('agent.weekdays').split(','));
	function when(s: Schedule): string {
		const base = t(everyText[s.every] ?? 'agent.everyDaily');
		if (s.every === 'trigger' && s.trigger)
			return `${base}: ${s.trigger.entity} = ${s.trigger.state}`;
		if (s.every === 'weekly') return `${base} · ${weekdays[s.weekday ?? 0] ?? ''} ${s.at}`;
		if (s.every === 'monthly') return `${base} · ${s.day}. · ${s.at}`;
		return s.at ? `${base} · ${s.at}` : base;
	}
	function editSchedule(s?: Schedule) {
		editing = { kind: 'schedule', id: s?.id ?? 'new' };
		draft = {
			name: s?.name ?? '',
			task: s?.task ?? '',
			every: s?.every ?? 'weekly',
			at: s?.at ?? '08:00',
			weekday: String(s?.weekday ?? 1),
			day: String(s?.day ?? 1),
			entity: s?.trigger?.entity ?? '',
			state: s?.trigger?.state ?? '',
			enabled: s ? String(s.enabled) : 'true',
			button: String(!!s?.button),
		};
	}
	function scheduleBody() {
		const trigger = draft.every === 'trigger' ? { entity: draft.entity, state: draft.state } : null;
		return {
			name: draft.name,
			task: draft.task,
			every: draft.every === 'trigger' ? 'daily' : draft.every,
			at: draft.at,
			weekday: Number(draft.weekday),
			day: Number(draft.day),
			enabled: draft.enabled !== 'false',
			button: draft.button === 'true',
			trigger,
		};
	}
</script>

<Dialog title={t('agent.knowledge')} wide {onclose}>
	<Tabs
		label={t('agent.knowledge')}
		value={tab}
		onchange={(id) => (tab = id)}
		tabs={[
			{ id: 'memory', label: t('agent.memory'), badge: memory.length || '' },
			{
				id: 'skills',
				label: t('agent.skills'),
				badge: skills.filter((s) => !s.builtIn).length || '',
			},
			{ id: 'schedules', label: t('agent.schedules'), badge: schedules.length || '' },
		]}
	/>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

	{#if tab === 'memory'}
		<p class="muted">{t('agent.memoryIntro')}</p>
		<ul class="rows">
			{#each memory as m (m.id)}
				<li>
					{#if editing?.kind === 'memory' && editing.id === m.id}
						<input class="field grow" bind:value={draft.text} maxlength="500" />
						<button
							type="button"
							class="btn btn-primary"
							onclick={() => run(() => api.put(`/agent/memory/${m.id}`, { text: draft.text }))}
							>{t('common.save')}</button
						>
						<button type="button" class="btn" onclick={() => (editing = null)}
							>{t('common.cancel')}</button
						>
					{:else}
						<span class="grow">
							{m.text}
							<span class="muted small"
								>{formatDateTime(new Date(m.at))}{m.project
									? ' · ' + projectName(m.project)
									: ''}{#if m.untrusted}
									· <span class="chip warn" title={t('agent.untrustedHint')}
										>{t('agent.untrusted')}</span
									>{/if}</span
							>
						</span>
						{#if m.chat}<button type="button" class="btn" onclick={() => onopenchat(m.chat!)}
								>{t('agent.source')}</button
							>{/if}
						<button
							type="button"
							class="btn"
							onclick={() => {
								editing = { kind: 'memory', id: m.id };
								draft = { text: m.text };
							}}>{t('agent.edit')}</button
						>
						<button
							type="button"
							class="btn btn-danger-outline"
							onclick={() => run(() => api.delete(`/agent/memory/${m.id}`))}
							>{t('common.delete')}</button
						>
					{/if}
				</li>
			{:else}
				<li class="muted">{t('agent.memoryEmpty')}</li>
			{/each}
		</ul>
		<div class="row-actions">
			{#if editing?.kind === 'memory' && editing.id === 'new'}
				<input
					class="field grow"
					bind:value={draft.text}
					maxlength="500"
					placeholder={t('agent.memoryNewHint')}
				/>
				<button
					type="button"
					class="btn btn-primary"
					onclick={() => run(() => api.put('/agent/memory/new', { text: draft.text }))}
					>{t('common.save')}</button
				>
			{:else}
				<button
					type="button"
					class="btn"
					onclick={() => {
						editing = { kind: 'memory', id: 'new' };
						draft = { text: '' };
					}}>{t('agent.memoryAdd')}</button
				>
				{#if memory.length}<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => run(() => api.delete('/agent/memory/all'))}
						>{t('agent.memoryClear')}</button
					>{/if}
			{/if}
		</div>
	{:else if tab === 'skills'}
		<p class="muted">{t('agent.skillsIntro')}</p>
		<ul class="rows">
			{#each skills as s (s.id)}
				<li class="skill">
					{#if editing?.kind === 'skill' && editing.id === s.id}
						<div class="grow form">
							<input class="field" bind:value={draft.name} maxlength="80" />
							<input class="field" bind:value={draft.when} placeholder={t('agent.skillWhen')} />
							<textarea class="field" rows="5" bind:value={draft.steps}></textarea>
							<div class="row-actions">
								<button
									type="button"
									class="btn btn-primary"
									onclick={() => run(() => api.put(`/agent/skills/${s.id}`, draft))}
									>{t('common.save')}</button
								>
								<button type="button" class="btn" onclick={() => (editing = null)}
									>{t('common.cancel')}</button
								>
							</div>
						</div>
					{:else}
						<details class="grow">
							<summary
								><strong>{s.name}</strong>
								{#if s.builtIn}<span class="chip">{t('agent.builtIn')}</span>{/if}
								{#if s.untrusted}<span class="chip warn" title={t('agent.untrustedHint')}
										>{t('agent.untrusted')}</span
									>{/if}
								{#if s.disabled}<span class="chip">{t('agent.off')}</span>{/if}
								<span class="muted small">{s.when}</span></summary
							>
							<pre>{s.steps}</pre>
						</details>
						<label class="check"
							><input
								type="checkbox"
								checked={!s.disabled}
								onchange={(e) =>
									run(() =>
										api.put(`/agent/skills/${s.id}`, { ...s, disabled: !e.currentTarget.checked }),
									)}
							/><span>{t('agent.on')}</span></label
						>
						{#if !s.builtIn}
							<button
								type="button"
								class="btn"
								onclick={() => {
									editing = { kind: 'skill', id: s.id };
									draft = { name: s.name, when: s.when, steps: s.steps };
								}}>{t('agent.edit')}</button
							>
							<button
								type="button"
								class="btn btn-danger-outline"
								onclick={() => run(() => api.delete(`/agent/skills/${s.id}`))}
								>{t('common.delete')}</button
							>
						{/if}
					{/if}
				</li>
			{/each}
		</ul>
		<div class="row-actions">
			{#if editing?.kind === 'skill' && editing.id === 'new'}
				<div class="grow form">
					<input
						class="field"
						bind:value={draft.name}
						maxlength="80"
						placeholder={t('agent.skillName')}
					/>
					<input class="field" bind:value={draft.when} placeholder={t('agent.skillWhen')} />
					<textarea
						class="field"
						rows="5"
						bind:value={draft.steps}
						placeholder={t('agent.skillSteps')}></textarea>
					<div class="row-actions">
						<button
							type="button"
							class="btn btn-primary"
							onclick={() => run(() => api.put('/agent/skills/new', draft))}
							>{t('common.save')}</button
						>
						<button type="button" class="btn" onclick={() => (editing = null)}
							>{t('common.cancel')}</button
						>
					</div>
				</div>
			{:else}
				<button
					type="button"
					class="btn"
					onclick={() => {
						editing = { kind: 'skill', id: 'new' };
						draft = { name: '', when: '', steps: '' };
					}}>{t('agent.skillAdd')}</button
				>
			{/if}
		</div>
	{:else}
		<p class="muted">{t('agent.schedulesIntro')}</p>
		<ul class="rows">
			{#each schedules as s (s.id)}
				<li>
					<span class="grow">
						<strong>{s.name}</strong>
						{#if !s.enabled}<span class="chip">{t('agent.off')}</span>{/if}
						<span class="muted small"
							>{when(s)}{s.lastRun
								? ' · ' + t('agent.lastRun', { when: formatDateTime(new Date(s.lastRun)) })
								: ''}{s.lastRun ? (s.lastOk ? ' ✓' : ' !') : ''}</span
						>
					</span>
					{#if s.chat}<button type="button" class="btn" onclick={() => onopenchat(s.chat!)}
							>{t('agent.lastResult')}</button
						>{/if}
					<button
						type="button"
						class="btn"
						onclick={() => run(() => api.post(`/agent/schedules/${s.id}/run`))}
						>{t('agent.runNow')}</button
					>
					<button type="button" class="btn" onclick={() => editSchedule(s)}
						>{t('agent.edit')}</button
					>
					<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => run(() => api.delete(`/agent/schedules/${s.id}`))}
						>{t('common.delete')}</button
					>
				</li>
			{:else}
				<li class="muted">{t('agent.schedulesEmpty')}</li>
			{/each}
		</ul>
		{#if editing?.kind === 'schedule'}
			<div class="form box">
				<label class="stack"
					><span>{t('agent.scheduleName')}</span><input
						class="field"
						bind:value={draft.name}
						maxlength="80"
					/></label
				>
				<label class="stack"
					><span>{t('agent.scheduleTask')}</span><textarea
						class="field"
						rows="3"
						bind:value={draft.task}></textarea></label
				>
				<div class="line">
					<select class="field" bind:value={draft.every} aria-label={t('agent.scheduleEvery')}>
						{#each ['hourly', 'daily', 'weekly', 'monthly', 'trigger'] as e (e)}<option value={e}
								>{t(everyText[e])}</option
							>{/each}
					</select>
					{#if draft.every === 'weekly'}
						<select class="field" bind:value={draft.weekday} aria-label={t('agent.weekday')}>
							{#each weekdays as d, i (i)}<option value={String(i)}>{d}</option>{/each}
						</select>
					{/if}
					{#if draft.every === 'monthly'}
						<input
							class="field small-in"
							type="number"
							min="1"
							max="28"
							bind:value={draft.day}
							aria-label={t('agent.day')}
						/>
					{/if}
					{#if draft.every !== 'hourly' && draft.every !== 'trigger'}
						<input
							class="field small-in"
							type="time"
							bind:value={draft.at}
							aria-label={t('agent.time')}
						/>
					{/if}
					{#if draft.every === 'trigger'}
						<input
							class="field"
							bind:value={draft.entity}
							placeholder="binary_sensor.haustuer"
							aria-label={t('agent.triggerEntity')}
						/>
						<input
							class="field small-in"
							bind:value={draft.state}
							placeholder="on"
							aria-label={t('agent.triggerState')}
						/>
					{/if}
				</div>
				<label class="check"
					><input
						type="checkbox"
						checked={draft.enabled !== 'false'}
						onchange={(e) => (draft.enabled = String(e.currentTarget.checked))}
					/><span>{t('agent.on')}</span></label
				>
				<label class="check"
					><input
						type="checkbox"
						checked={draft.button === 'true'}
						onchange={(e) => (draft.button = String(e.currentTarget.checked))}
					/><span>{t('agent.scheduleButton')}</span></label
				>
				<div class="row-actions">
					<button
						type="button"
						class="btn btn-primary"
						onclick={() => run(() => api.put(`/agent/schedules/${editing!.id}`, scheduleBody()))}
						>{t('common.save')}</button
					>
					<button type="button" class="btn" onclick={() => (editing = null)}
						>{t('common.cancel')}</button
					>
				</div>
			</div>
		{:else}
			<div class="row-actions">
				<button type="button" class="btn" onclick={() => editSchedule()}
					>{t('agent.scheduleAdd')}</button
				>
			</div>
		{/if}
	{/if}
	<div class="dialog-actions end">
		<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
	</div>
</Dialog>

<style>
	.rows {
		list-style: none;
		margin: var(--space-2) 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		max-height: 50vh;
		overflow-y: auto;
	}
	.rows li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: 8px 0;
		border-bottom: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.grow {
		flex: 1;
		min-width: 200px;
	}
	.small {
		font-size: var(--text-sm);
		display: block;
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.box {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: var(--space-3);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.line {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.small-in {
		width: 110px;
	}
	.row-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		align-items: center;
	}
	pre {
		white-space: pre-wrap;
		font-size: var(--text-sm);
		margin: 6px 0;
		color: var(--muted);
	}
	summary {
		cursor: pointer;
		display: flex;
		gap: 8px;
		align-items: baseline;
		flex-wrap: wrap;
	}
	.end {
		display: flex;
		justify-content: flex-end;
		margin-top: var(--space-3);
	}
</style>
