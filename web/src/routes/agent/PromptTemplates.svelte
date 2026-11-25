<script lang="ts">
	import { onMount } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { t, apiErrorText } from '#lib/i18n/index.svelte.ts';
	type Template = { id: string; name: string; text: string };
	let {
		text = '',
		occupied = false,
		onuse,
		onclose,
	}: {
		occupied?: boolean;
		text?: string;
		onuse: (text: string) => void;
		onclose: () => void;
	} = $props();
	const path = '/agent/prompt-templates';
	let list = $state<Template[]>([]),
		name = $state(''),
		busy = $state(false),
		error = $state(''),
		confirm = $state('');
	async function change(action: 'get' | 'save' | 'remove', id = '') {
		busy = true;
		error = '';
		try {
			const result =
				action === 'get'
					? await api.get<{ templates: Template[] }>(path)
					: action === 'save'
						? await api.post<{ templates: Template[] }>(path, { name, text })
						: await api.delete<{ templates: Template[] }>(path + '/' + encodeURIComponent(id));
			list = result.templates;
			confirm = '';
			if (action === 'save') onclose();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	onMount(() => {
		if (!text) void change('get');
	});
</script>

<Dialog title={t(text ? 'agent.library.save' : 'agent.library.templates')} {onclose}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if text}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				void change('save');
			}}
		>
			<label>{t('auth.name')}<input class="field" bind:value={name} maxlength="80" /></label>
			<p class="sample">{text}</p>
			<button class="btn btn-primary" disabled={busy || !name.trim()}>{t('common.save')}</button>
		</form>
	{:else}
		{#if busy}<p role="status">{t('common.loading')}</p>{:else if !list.length && !error}<p>
				{t('agent.library.empty')}
			</p>{/if}
		{#if occupied}<p role="status">{t('agent.library.draftFirst')}</p>{/if}
		{#each list as entry (entry.id)}<div class="template">
				<strong>{entry.name}</strong>
				<p class="sample">{entry.text}</p>
				<div class="actions">
					<button class="btn" disabled={busy || occupied} onclick={() => onuse(entry.text)}
						>{t('agent.library.use')}</button
					>
					{#if confirm === entry.id}<button
							class="btn danger"
							disabled={busy}
							onclick={() => change('remove', entry.id)}>{t('common.delete')}</button
						><button class="btn" onclick={() => (confirm = '')}>{t('common.cancel')}</button
						>{:else}<button class="btn" disabled={busy} onclick={() => (confirm = entry.id)}
							>{t('editor.remove')}</button
						>{/if}
				</div>
			</div>{/each}
	{/if}
</Dialog>

<style>
	label {
		display: grid;
		gap: 0.35rem;
	}
	.sample {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 10rem;
		overflow: auto;
	}
	.template {
		padding: 0.75rem 0;
		border-bottom: 1px solid var(--line);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
	}
</style>
