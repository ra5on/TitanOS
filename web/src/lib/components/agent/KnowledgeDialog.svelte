<script lang="ts">
	// What the KI-Agent learned and plans (PLAN 44, "Lernen"): the memory
	// (facts it remembered, changeable and removable), the skills (built-in
	// ones can be switched off, own ones changed) and the recurring tasks.
	// Entries written while the chat had read outside content carry a mark.
	import Dialog from '#lib/components/Dialog.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import NASMap from './NASMap.svelte';
	import Planned from './Planned.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Memory, Project, Skill } from '#lib/agent.ts';

	let {
		tab: startTab = 'memory',
		only = false,
		admin = false,
		projects = [],
		onopenchat,
		onclose,
	}: {
		tab?: 'memory' | 'skills' | 'schedules' | 'map' | 'me' | 'log';
		/** PLAN 53: show only this part, without the tabs (from the settings or "Geplant"). */
		only?: boolean;
		admin?: boolean;
		projects?: Project[];
		onopenchat: (id: string) => void;
		onclose: () => void;
	} = $props();

	let tab = $state((() => startTab)());
	let memory = $state<Memory[]>([]);
	let skills = $state<Skill[]>([]);
	let planned = $state(0);
	let error = $state('');
	let editing = $state<{ kind: 'memory' | 'skill'; id: string } | null>(null);
	let draft = $state<Record<string, string>>({});
	// Home Assistant connections for triggers (PLAN §45), by name.
	let haConns = $state<{ id: string; name: string }[]>([]);
	$effect(() => {
		api
			.get<{ connections?: { id: string; name: string }[] }>('/homeassistant/connection-names')
			.then(
				(res) => (haConns = res.connections ?? []),
				() => (haConns = []),
			);
	});

	let profile = $state('');
	let log = $state<{ at: string; user: string; action: string; target?: string; ok: boolean }[]>(
		[],
	);
	$effect(() => {
		if (tab === 'me')
			void api
				.get<{ instruction?: string }>('/agent/profile')
				.then((p) => (profile = p.instruction ?? ''))
				.catch(() => {});
		if (tab === 'log')
			void api
				.get<{ entries: typeof log }>('/activity?via=agent&days=30&limit=200')
				.then((r) => (log = r.entries))
				.catch(() => (log = []));
	});

	async function load() {
		error = '';
		try {
			[memory, skills] = await Promise.all([
				api.get<{ memory: Memory[] }>('/agent/memory').then((r) => r.memory),
				api.get<{ skills: Skill[] }>('/agent/skills').then((r) => r.skills),
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
</script>

<Dialog
	title={only
		? {
				memory: t('agent.memory'),
				skills: t('agent.skills'),
				schedules: t('agent.schedules'),
				map: t('agent.map'),
				me: t('agent.me'),
				log: t('agent.log'),
			}[tab]
		: t('agent.knowledge')}
	wide
	{onclose}
>
	{#if !only}<Tabs
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
				{ id: 'schedules', label: t('agent.schedules'), badge: planned || '' },
				{ id: 'map', label: t('agent.map') },
				{ id: 'me', label: t('agent.me') },
				...(admin ? [{ id: 'log' as const, label: t('agent.log') }] : []),
			]}
		/>{/if}
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
	{:else if tab === 'map'}
		<NASMap {admin} />
	{:else if tab === 'me'}
		<p class="muted">{t('agent.meIntro')}</p>
		<textarea
			class="field me"
			rows="6"
			maxlength="4000"
			bind:value={profile}
			placeholder={t('agent.meHint')}></textarea>
		<div class="row-actions">
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => run(() => api.put('/agent/profile', { instruction: profile }))}
				>{t('common.save')}</button
			>
		</div>
	{:else if tab === 'log'}
		<p class="muted">{t('agent.logIntro')}</p>
		<ul class="rows">
			{#each log as e, i (i)}
				<li>
					<span class="grow"
						><strong>{e.action}</strong>
						{e.target ?? ''}<span class="muted small"
							>{formatDateTime(new Date(e.at))} · {e.user} · {e.ok ? '✓' : t('agent.failed')}</span
						></span
					>
				</li>
			{:else}
				<li class="muted">{t('agent.logEmpty')}</li>
			{/each}
		</ul>
	{:else}
		<Planned {projects} {onopenchat} oncount={(n) => (planned = n)} />
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
		padding: var(--space-2) 0;
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
	.me {
		width: 100%;
		margin: var(--space-2) 0;
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
		margin: var(--space-1-5) 0;
		color: var(--muted);
	}
	summary {
		cursor: pointer;
		display: flex;
		gap: var(--space-2);
		align-items: baseline;
		flex-wrap: wrap;
	}
	.end {
		display: flex;
		justify-content: flex-end;
		margin-top: var(--space-3);
	}
</style>
