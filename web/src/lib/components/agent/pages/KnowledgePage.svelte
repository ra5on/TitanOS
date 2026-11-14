<script lang="ts">
	// "Wissen" of the KI-Agent as a page (PLAN 90-4): what it knows besides
	// the memory – the person's own instruction and those of the projects,
	// the skills (procedures it follows; built-in ones can be switched off,
	// own ones changed), the map of the NAS, the places it may look things up
	// in, and for owners and administrators what it changed in 30 days.
	// Each stood in a dialog or in the settings before; here once.
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import NASMap from '../NASMap.svelte';
	import { api, type FileRoot } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		defaultInitiative,
		sanitizeInitiative,
		type Initiative,
		type Project,
		type Skill,
	} from '#lib/agent.ts';
	import PageShell from './PageShell.svelte';

	let {
		admin = false,
		projects = [],
		phone = false,
		onunfold,
		oneditproject,
		onopenproject,
	}: {
		/** Opens a project: its chats, the files the agent made in it, its memory. */
		onopenproject: (p: Project) => void;
		admin?: boolean;
		projects?: Project[];
		phone?: boolean;
		onunfold?: () => void;
		oneditproject: (p: Project) => void;
	} = $props();

	let query = $state('');
	let error = $state('');
	let skills = $state<Skill[]>([]);
	let profile = $state('');
	let savedProfile = $state('');
	let log = $state<{ at: string; user: string; action: string; target?: string; ok: boolean }[]>(
		[],
	);
	let roots = $state<FileRoot[]>([]);
	let editing = $state<{ id: string; name: string; when: string; steps: string } | null>(null);
	let removing = $state<Skill | null>(null);

	async function loadSkills() {
		try {
			skills = (await api.get<{ skills: Skill[] }>('/agent/skills')).skills;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void loadSkills();
		api
			.get<{ instruction?: string }>('/agent/profile')
			.then((p) => (profile = savedProfile = p.instruction ?? ''))
			.catch(() => {});
		api
			.get<FileRoot[]>('/files/roots')
			.then((r) => (roots = r.filter((x) => x.kind !== 'stick')))
			.catch(() => (roots = []));
		if (admin)
			api
				.get<{ entries: typeof log }>('/activity?via=agent&days=30&limit=200')
				.then((r) => (log = r.entries ?? []))
				.catch(() => (log = []));
	});
	async function run(fn: () => Promise<unknown>) {
		error = '';
		try {
			await fn();
			editing = null;
			await loadSkills();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function saveProfile() {
		error = '';
		try {
			await api.put('/agent/profile', { instruction: profile });
			savedProfile = profile;
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	// The places to look things up in are part of the initiative settings.
	let ini = $state<Initiative>(defaultInitiative());
	let iniLoaded = $state(false);
	$effect(() => {
		api
			.get<unknown>('/agent/initiative')
			.then((v) => {
				ini = sanitizeInitiative(v);
				iniLoaded = true;
			})
			.catch((e) => (error = apiErrorText(e)));
	});
	const searched = (id: string) => ini.search.some((s) => s.root === id && !s.path);
	function toggleRoot(id: string, on: boolean) {
		if (!iniLoaded) return;
		ini.search = on
			? [...ini.search.filter((s) => s.root !== id), { root: id, path: '' }]
			: ini.search.filter((s) => s.root !== id);
		api.put('/agent/initiative', $state.snapshot(ini)).catch((e) => (error = apiErrorText(e)));
	}

	const q = $derived(query.trim().toLowerCase());
	const hit = (...texts: (string | undefined)[]) =>
		!q || texts.some((x) => (x ?? '').toLowerCase().includes(q));
	const shownSkills = $derived(skills.filter((s) => hit(s.name, s.when, s.steps)));
	const withText = $derived(projects.filter((p) => p.instructions?.trim()));
	const shownProjects = $derived(projects.filter((p) => !p.owner && hit(p.name, p.instructions)));
	const lines = (text: string) => text.split('\n').filter((l) => l.trim()).length;
	const stepCount = (s: Skill) => lines(s.steps);
	const firstLine = (text: string) => text.trim().split('\n')[0] ?? '';
	const origin = (s: Skill) =>
		s.builtIn ? t('agent.builtIn') : s.untrusted ? t('agent.untrusted') : t('agent.know.own');
</script>

{#snippet skillForm()}
	<form
		class="form"
		onsubmit={(e) => {
			e.preventDefault();
			const { id, ...body } = editing!;
			void run(() => api.put(`/agent/skills/${id}`, body));
		}}
	>
		<input
			class="field"
			bind:value={editing!.name}
			maxlength="80"
			aria-label={t('agent.skillName')}
			placeholder={t('agent.skillName')}
		/>
		<input
			class="field"
			bind:value={editing!.when}
			aria-label={t('agent.skillWhen')}
			placeholder={t('agent.skillWhen')}
		/>
		<textarea
			class="field"
			rows="6"
			bind:value={editing!.steps}
			aria-label={t('agent.skillSteps')}
			placeholder={t('agent.skillSteps')}></textarea>
		<div class="row-actions">
			<button type="submit" class="btn btn-primary">{t('common.save')}</button>
			<button type="button" class="btn" onclick={() => (editing = null)}
				>{t('common.cancel')}</button
			>
		</div>
	</form>
{/snippet}

<PageShell title={t('agent.knowledge')} lead={t('agent.know.lead')} {phone} {onunfold}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<label class="search">
		<Icon name="search" size={16} />
		<input
			type="search"
			bind:value={query}
			placeholder={t('agent.know.search')}
			aria-label={t('agent.know.search')}
		/>
	</label>

	{#if hit(t('agent.me'), profile) || shownProjects.length}
		<SettingGroup title={t('agent.know.given')}>
			{#if hit(t('agent.me'), profile)}
				<SettingRow
					id="me"
					icon="chat"
					title={t('agent.me')}
					desc={firstLine(savedProfile) ? `„${firstLine(savedProfile)}“` : t('agent.meIntro')}
					summary={phone
						? ''
						: savedProfile.trim()
							? lines(savedProfile) === 1
								? t('agent.know.stepOne')
								: t('agent.know.lines', { count: lines(savedProfile) })
							: t('agent.know.empty')}
				>
					<div class="srow-block">
						<p class="muted">{t('agent.meIntro')}</p>
						<textarea
							class="field wide"
							rows="6"
							maxlength="4000"
							bind:value={profile}
							aria-label={t('agent.me')}
							placeholder={t('agent.meHint')}></textarea>
						<div class="row-actions">
							<button
								type="button"
								class="btn btn-primary"
								disabled={profile === savedProfile}
								onclick={saveProfile}>{t('common.save')}</button
							>
						</div>
					</div>
				</SettingRow>
			{/if}
			{#if projects.some((p) => !p.owner) && (shownProjects.length || !q)}
				<SettingRow
					id="projects"
					icon="folder"
					title={t('agent.know.projects')}
					desc={withText.length
						? withText.map((p) => p.name).join(', ')
						: t('agent.know.projectsNone')}
					summary={phone
						? ''
						: t('agent.know.projectsOf', { count: withText.length, total: projects.length })}
					open={!!q}
				>
					{#each shownProjects as p (p.id)}
						<div class="srow-sub">
							<span class="p-text"
								><strong>{p.icon ?? '📁'} {p.name}</strong><small
									>{p.instructions?.trim() || t('agent.know.projectEmpty')}</small
								></span
							>
							<button type="button" class="btn" onclick={() => onopenproject(p)}
								>{t('agent.know.projectOpen')}</button
							>
							<button type="button" class="btn" onclick={() => oneditproject(p)}
								>{t('agent.edit')}</button
							>
						</div>
					{/each}
				</SettingRow>
			{/if}
		</SettingGroup>
	{/if}

	{#if shownSkills.length || !q}
		<SettingGroup title={t('agent.know.skills')}>
			{#snippet tools()}<button
					type="button"
					class="btn"
					onclick={() => (editing = { id: 'new', name: '', when: '', steps: '' })}
					><Icon name="plus" size={15} />{t('agent.know.skillAdd')}</button
				>{/snippet}
			{#if editing?.id === 'new'}
				<div class="srow">
					<div class="srow-head">
						<span class="srow-ico"><Icon name="zap" size={19} /></span>
						<div class="srow-text">{@render skillForm()}</div>
					</div>
				</div>
			{/if}
			{#each shownSkills as s (s.id)}
				<SettingRow
					id="skill-{s.id}"
					icon="zap"
					title={s.name}
					desc={`${s.when} · ${stepCount(s) === 1 ? t('agent.know.stepOne') : t('agent.know.steps', { count: stepCount(s) })}`}
					summary={phone ? '' : origin(s)}
					open={editing?.id === s.id}
				>
					{#snippet badge()}{#if s.untrusted}<span
								class="chip warn"
								title={t('agent.untrustedHint')}>{t('agent.untrusted')}</span
							>{/if}{#if s.disabled}<span class="chip">{t('agent.off')}</span>{/if}{/snippet}
					{#snippet control()}<input
							type="checkbox"
							class="switch"
							role="switch"
							aria-label={t('agent.know.skillOn', { name: s.name })}
							checked={!s.disabled}
							onchange={(e) =>
								run(() =>
									api.put(`/agent/skills/${s.id}`, { ...s, disabled: !e.currentTarget.checked }),
								)}
						/>{/snippet}
					<div class="srow-block">
						{#if editing?.id === s.id}
							{@render skillForm()}
						{:else}
							<pre>{s.steps}</pre>
							{#if !s.builtIn}
								<div class="row-actions">
									<button
										type="button"
										class="btn"
										onclick={() =>
											(editing = { id: s.id, name: s.name, when: s.when, steps: s.steps })}
										>{t('agent.edit')}</button
									>
									<button
										type="button"
										class="btn btn-danger-outline"
										onclick={() => (removing = s)}>{t('common.delete')}</button
									>
								</div>
							{/if}
						{/if}
					</div>
				</SettingRow>
			{:else}
				<p class="muted">{t('agent.know.noSkill')}</p>
			{/each}
		</SettingGroup>
	{/if}

	{#if !q || hit(t('agent.map'), t('agent.know.docs'), t('agent.log'))}
		<SettingGroup title={t('agent.know.nas')}>
			{#if hit(t('agent.map'))}
				<SettingRow id="map" icon="disk" title={t('agent.map')} desc={t('agent.know.mapDesc')}>
					<div class="srow-block"><NASMap {admin} /></div>
				</SettingRow>
			{/if}
			{#if hit(t('agent.know.docs'))}
				<SettingRow
					id="docs"
					icon="fileText"
					title={t('agent.know.docs')}
					desc={t('agent.init.searchHint')}
					summary={phone
						? ''
						: t('agent.know.docsOf', {
								count: roots.filter((r) => searched(r.id)).length,
								total: roots.length,
							})}
				>
					{#each roots as r (r.id)}
						<label class="srow-sub">
							<span>{r.name}</span>
							<input
								type="checkbox"
								class="switch"
								role="switch"
								disabled={!iniLoaded}
								checked={searched(r.id)}
								onchange={(e) => toggleRoot(r.id, e.currentTarget.checked)}
							/>
						</label>
					{:else}
						<div class="srow-block"><p class="muted">{t('agent.know.docsNone')}</p></div>
					{/each}
				</SettingRow>
			{/if}
			{#if admin && hit(t('agent.log'))}
				<SettingRow
					id="log"
					icon="list"
					title={t('agent.log')}
					desc={t('agent.logIntro')}
					summary={phone ? '' : t('agent.know.logCount', { count: log.length })}
				>
					<div class="srow-block">
						<ul class="log">
							{#each log as e, i (i)}
								<li>
									<strong>{e.action}</strong>
									{e.target ?? ''}
									<small
										>{formatDateTime(new Date(e.at))} · {e.user} · {e.ok
											? '✓'
											: t('agent.failed')}</small
									>
								</li>
							{:else}
								<li class="muted">{t('agent.logEmpty')}</li>
							{/each}
						</ul>
					</div>
				</SettingRow>
			{/if}
		</SettingGroup>
	{/if}
</PageShell>

{#if removing}
	{@const s = removing}
	<Dialog title={t('agent.know.skillDelete')} onclose={() => (removing = null)}>
		<p>„{s.name}“</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					const id = s.id;
					removing = null;
					void run(() => api.delete(`/agent/skills/${id}`));
				}}>{t('common.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.search {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 36px;
		padding: 0 var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--bg);
		color: var(--muted);
	}
	.search:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	.search input {
		flex: 1;
		min-width: 0;
		min-height: 34px;
		border: 0;
		outline: 0;
		padding: 0;
		background: none;
		color: var(--text);
		font: inherit;
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.form .field,
	.field.wide {
		width: 100%;
	}
	.row-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	pre {
		margin: 0 0 var(--space-2);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-family: inherit;
		font-size: var(--text-base);
		color: var(--muted);
	}
	.p-text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.p-text small,
	.log small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.log {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 50vh;
		overflow-y: auto;
	}
	.log li {
		padding: var(--space-2) 0;
		border-bottom: 1px solid var(--border);
		overflow-wrap: anywhere;
	}
	.log small {
		display: block;
	}
	@media (max-width: 760px) {
		.search {
			min-height: 44px;
		}
		.search input {
			font-size: max(16px, 1rem);
		}
	}
</style>
