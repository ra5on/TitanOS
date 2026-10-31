<script lang="ts">
	// The plugin sources of the community: listed, added by their address,
	// removed after asking.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import ProblemNotice from './ProblemNotice.svelte';
	import type { Problem, Source } from './plugins.ts';

	type Props = {
		sources: Source[];
		url: string;
		adding: boolean;
		error: Problem | null;
		onclose: () => void;
		onadd: () => void;
		onremove: (source: Source) => void;
	};
	let { sources, url = $bindable(), adding, error, onclose, onadd, onremove }: Props = $props();
</script>

<Dialog title={t('plugins.sourceListTitle')} locked={adding} {onclose}>
	<p class="text">{t('plugins.sourcesText')}</p>
	{#if sources.length}
		<ul class="sources">
			{#each sources as source (source.id)}
				<li class="divided">
					<div class="about">
						<span class="name">{source.name}</span>
						<span class="muted mono path">{source.url}</span>
						{#if source.error}<span class="notice-warn"
								>{t('plugins.sourceError')} {source.error}</span
							>{:else}<span class="muted"
								>{t('plugins.sourcePlugins', { count: source.plugins })}</span
							>{/if}
					</div>
					<button type="button" class="btn" onclick={() => onremove(source)} disabled={adding}
						>{t('plugins.sourceRemove')}</button
					>
				</li>
			{/each}
		</ul>
	{:else}
		<p class="muted">{t('plugins.sourcesEmpty')}</p>
	{/if}
	<form class="stack" onsubmit={(event) => (event.preventDefault(), onadd())}>
		<label class="stack"
			><span>{t('plugins.sourceURL')}</span>
			<input
				class="field mono"
				type="url"
				placeholder="https://example.org/plugins/plugins.json"
				bind:value={url}
				disabled={adding}
			/>
		</label>
		<p class="muted note">{t('plugins.sourceFormat')}</p>
		<div class="row-actions">
			<button type="submit" class="btn btn-primary" disabled={adding || !url.trim()}
				>{t(adding ? 'plugins.sourceAdding' : 'plugins.sourceAdd')}</button
			>
		</div>
	</form>
	{#if error}<ProblemNotice problem={error} />{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose} disabled={adding}
			>{t('common.close')}</button
		>
	</div>
</Dialog>

<style>
	.about {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.note {
		font-size: 0.8125rem;
	}
	.path {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.sources {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.sources li {
		display: flex;
		gap: 0.75rem;
		align-items: center;
		justify-content: space-between;
		padding: 0.5rem 0;
	}
	.sources .about {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.sources .path {
		overflow-wrap: anywhere;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.text {
		font-size: 0.875rem;
	}
</style>
