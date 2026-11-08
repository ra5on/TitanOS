<script lang="ts">
	// Create or change a project of the KI-Agent: name, symbol, color, the
	// default place for new files (a named location) and instructions the
	// agent gets in every chat of the project. Removing keeps the chats.
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { Location, Project } from '#lib/agent.ts';

	let {
		project = null,
		locations = [],
		onsaved,
		onclose,
	}: {
		project?: Project | null;
		locations?: Location[];
		onsaved: (p: Project | null) => void;
		onclose: () => void;
	} = $props();

	// Colors from the theme accents, so they fit every theme.
	const COLORS = ['', '#3b82f6', '#8b5cf6', '#10b981', '#f59e0b', '#ec4899', '#64748b'];
	const start = (() => project)();
	let name = $state(start?.name ?? '');
	let icon = $state(start?.icon ?? '');
	let color = $state(start?.color ?? '');
	let location = $state(start?.location ?? '');
	let instructions = $state(start?.instructions ?? '');
	// Sharing with other accounts of the agent (PLAN 51).
	let members = $state<string[]>([...(start?.members ?? [])]);
	let accounts = $state<string[] | null>(null);
	const shared = !!start?.owner;
	$effect(() => {
		api
			.get<{ accounts: string[] }>('/agent/accounts')
			.then((r) => (accounts = r.accounts))
			.catch(() => (accounts = []));
	});
	let busy = $state(false);
	let error = $state('');
	let confirmRemove = $state(false);

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		const body = { name, icon, color, location, instructions, members };
		try {
			const saved = start
				? await api.put<Project>(`/agent/projects/${start.id}`, body)
				: await api.post<Project>('/agent/projects', body);
			onsaved(saved);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function remove() {
		if (!start) return;
		busy = true;
		try {
			await api.delete(`/agent/projects/${start.id}`);
			onsaved(null);
		} catch (e) {
			error = apiErrorText(e);
			busy = false;
		}
	}
</script>

<Dialog title={start ? t('agent.projectEdit') : t('agent.newProject')} locked={busy} {onclose}>
	<!-- PLAN 72: cards with rows like every settings card. -->
	<form class="form" onsubmit={save}>
		<section class="tile ui-card">
			<div class="set-row">
				<label class="set-label" for="project-name">{t('agent.projectName')}</label>
				<span class="set-control"
					><input id="project-name" class="field" bind:value={name} required maxlength="80" /></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="project-icon">{t('agent.projectIcon')}</label>
				<span class="set-control"
					><input
						id="project-icon"
						class="field icon"
						bind:value={icon}
						maxlength="4"
						placeholder="🏠"
					/></span
				>
			</div>
			<div class="set-row">
				<span class="set-label">{t('agent.projectColor')}</span>
				<span class="set-control">
					<span class="colors" role="radiogroup" aria-label={t('agent.projectColor')}>
						{#each COLORS as c (c)}
							<button
								type="button"
								role="radio"
								aria-checked={color === c}
								aria-label={c || t('agent.colorTheme')}
								class="swatch"
								style:background={c || 'var(--accent)'}
								onclick={() => (color = c)}
							></button>
						{/each}
					</span>
				</span>
			</div>
			<div class="set-row">
				<label class="set-label" for="project-location">{t('agent.projectLocation')}</label>
				<span class="set-control"
					><select id="project-location" class="field" bind:value={location}>
						<option value="">{t('agent.projectLocationNone')}</option>
						{#each locations as l (l.name)}<option value={l.name}>{l.name}</option>{/each}
					</select></span
				>
			</div>
		</section>
		<section class="tile ui-card">
			<div class="card-head">
				<h3><label for="project-instructions">{t('agent.projectInstructions')}</label></h3>
			</div>
			<div class="set-row wide">
				<textarea
					id="project-instructions"
					class="field"
					rows="5"
					maxlength="8000"
					bind:value={instructions}
					placeholder={t('agent.projectInstructionsHint')}></textarea>
			</div>
		</section>
		{#if shared}
			<p class="notice-warn">{t('agent.projectShared', { name: start?.owner ?? '' })}</p>
		{:else}
			<section class="tile ui-card">
				<div class="card-head"><h3>{t('agent.members')}</h3></div>
				{#if accounts === null}
					<p class="set-row wide muted">…</p>
				{:else if accounts.length === 0}
					<p class="set-row wide muted">{t('agent.membersNone')}</p>
				{:else}
					{#each accounts as a (a)}
						<div class="set-row">
							<label class="set-label" for={`member-${a}`}>{a}</label>
							<span class="set-control end"
								><input
									id={`member-${a}`}
									type="checkbox"
									class="switch"
									checked={members.includes(a)}
									onchange={(e) =>
										(members = e.currentTarget.checked
											? [...members, a]
											: members.filter((m) => m !== a))}
								/></span
							>
						</div>
					{/each}
				{/if}
				<p class="set-row wide muted hint">{t('agent.membersHint')}</p>
			</section>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			{#if start && !shared}
				{#if confirmRemove}
					<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
						>{t('agent.projectRemoveNow')}</button
					>
				{:else}
					<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => (confirmRemove = true)}>{t('agent.projectRemove')}</button
					>
				{/if}
			{/if}
			<span class="grow"></span>
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={busy || shared || !name.trim()}
				>{t('common.save')}</button
			>
		</div>
		{#if confirmRemove}<p class="muted">{t('agent.projectRemoveHint')}</p>{/if}
	</form>
</Dialog>

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
	}
	.tile.ui-card {
		flex-shrink: 0;
	}
	.set-row.wide {
		grid-template-columns: minmax(0, 1fr);
		margin: 0;
	}
	.set-control .field {
		width: 16rem;
		max-width: 100%;
	}
	.set-control .field.icon {
		width: 5rem;
	}
	textarea {
		width: 100%;
		box-sizing: border-box;
	}
	.hint {
		font-size: var(--text-sm);
	}
	.colors {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		min-height: var(--control-height);
	}
	.swatch {
		width: 26px;
		height: 26px;
		border-radius: 50%;
		border: 2px solid transparent;
		cursor: pointer;
	}
	.swatch[aria-checked='true'] {
		outline: 2px solid var(--text);
		outline-offset: 2px;
	}
	.grow {
		flex: 1;
	}
	.dialog-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
</style>
