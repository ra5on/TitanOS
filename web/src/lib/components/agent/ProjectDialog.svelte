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
	let busy = $state(false);
	let error = $state('');
	let confirmRemove = $state(false);

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		const body = { name, icon, color, location, instructions };
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
	<form class="form" onsubmit={save}>
		<label class="stack">
			<span>{t('agent.projectName')}</span>
			<input class="field" bind:value={name} required maxlength="80" />
		</label>
		<div class="two">
			<label class="stack">
				<span>{t('agent.projectIcon')}</span>
				<input class="field" bind:value={icon} maxlength="4" placeholder="🏠" />
			</label>
			<div class="stack">
				<span>{t('agent.projectColor')}</span>
				<div class="colors" role="radiogroup" aria-label={t('agent.projectColor')}>
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
				</div>
			</div>
		</div>
		<label class="stack">
			<span>{t('agent.projectLocation')}</span>
			<select class="field" bind:value={location}>
				<option value="">{t('agent.projectLocationNone')}</option>
				{#each locations as l (l.name)}<option value={l.name}>{l.name}</option>{/each}
			</select>
		</label>
		<label class="stack">
			<span>{t('agent.projectInstructions')}</span>
			<textarea
				class="field"
				rows="5"
				maxlength="8000"
				bind:value={instructions}
				placeholder={t('agent.projectInstructionsHint')}></textarea>
		</label>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			{#if start}
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
			<button type="submit" class="btn btn-primary" disabled={busy || !name.trim()}
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
		gap: var(--space-3);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.two {
		display: grid;
		grid-template-columns: 100px 1fr;
		gap: var(--space-3);
	}
	.colors {
		display: flex;
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
