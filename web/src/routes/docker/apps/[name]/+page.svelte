<script lang="ts">
	import DangerZone from '#lib/components/DangerZone.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import ViewSwitch from '#lib/components/appform/ViewSwitch.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { AppEditor } from './app-editor.svelte.ts';
	import AppHead from './AppHead.svelte';
	import EditorDialogs from './EditorDialogs.svelte';
	import ServiceFields from './ServiceFields.svelte';

	// State and actions live in app-editor.svelte.ts.
	const ed = new AppEditor();
</script>

<PageHead title={t('editor.title', { name: ed.name })}>
	{#snippet actions()}<ViewSwitch bind:advanced={ed.advanced} />{/snippet}
</PageHead>

<AppHead {ed} />

{#if ed.loadError}<div class="notice-error row-note" role="alert">
		<span>{ed.loadError}</span><button
			type="button"
			class="btn small"
			onclick={() => ed.loadCompose()}>{t('common.retry')}</button
		>
	</div>
{:else if !ed.loaded}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
{#if ed.yamlError}<div class="notice-error" role="alert">
		{t('editor.yamlError')}
		<pre>{ed.yamlError}</pre>
	</div>{/if}

{#if ed.form && ed.problems && ed.view === 'form' && ed.services.length > 1}
	<div class="segments services" role="radiogroup" aria-label={t('editor.service')}>
		{#each ed.services as entry (entry)}
			<button
				type="button"
				role="radio"
				aria-checked={ed.service === entry}
				onclick={() => ed.pickService(entry)}>{entry}</button
			>
		{/each}
	</div>
{/if}

{#snippet nameRow()}
	<FormRow
		label={t('apps.name')}
		hint={t('editor.location', { path: `/config/apps/${ed.name}/compose.yaml` })}
	>
		<p class="text mono">
			{ed.name}{#if ed.services.length > 1}<span class="muted">&nbsp;· {ed.service}</span>{/if}
		</p>
	</FormRow>
{/snippet}

{#if ed.loaded}
	<!-- PLAN 78-5: the form is no tile: groups of flat rows under plain
	     headings, the bar with "Apply" below them. -->
	<section class="form" aria-label={t('editor.title', { name: ed.name })}>
		{#if ed.view === 'compose'}
			<SettingGroup>
				{@render nameRow()}
				<FormRow wide label={t('appform.composeFile')}>
					<textarea
						class="field mono yaml"
						bind:value={ed.text}
						spellcheck="false"
						aria-label={t('editor.compose')}></textarea>
				</FormRow>
			</SettingGroup>
		{:else if ed.form && ed.problems}
			<ServiceFields {ed} bind:form={ed.form} problems={ed.problems} head={nameRow} />
		{:else}
			<SettingGroup>
				{@render nameRow()}
				{#if !ed.loadError && !ed.yamlError && ed.original}
					<p class="notice-warn">{t('editor.noServices')}</p>
				{/if}
			</SettingGroup>
		{/if}

		{#if ed.result}
			<div class="result {ed.result.ok ? 'notice-ok' : 'notice-error'}" role="status">
				{ed.result.text}{#if ed.result.detail}<pre>{ed.result.detail}</pre>{/if}
			</div>
		{/if}
		<div class="apply">
			<button type="button" class="btn btn-primary" onclick={ed.apply} disabled={ed.blocked}
				>{t('editor.apply')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={!ed.changed || ed.applying}
				onclick={() => (ed.confirmDiscard = true)}>{t('editor.discard')}</button
			>
			<a class="btn" href="/docker">{t('common.done')}</a>
			<span class="muted note" role="status"
				>{ed.applying
					? t('editor.applying')
					: ed.changed
						? t('editor.applyNote')
						: t('editor.unchanged')}</span
			>
			<button
				type="button"
				class="link"
				onclick={() => ed.show(ed.view === 'form' ? 'compose' : 'form')}
				>{t(ed.view === 'form' ? 'appform.editCompose' : 'apps.asForm')}</button
			>
		</div>
		<div class="danger">
			<DangerZone hint={t('docker.appRemoveHint')}>
				<button
					type="button"
					class="btn btn-danger"
					disabled={ed.applying || ed.removeBusy}
					onclick={() => ((ed.removeError = ''), (ed.removing = true))}>{t('docker.remove')}</button
				>
			</DangerZone>
		</div>
	</section>
{/if}

<EditorDialogs {ed} />

<style>
	.segments {
		display: inline-flex;
		align-self: flex-start;
		gap: var(--space-1);
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.yaml {
		min-height: 60vh;
		height: auto;
		padding: var(--space-3) var(--space-4);
		font-size: var(--text-base);
		line-height: 1.55;
		resize: vertical;
		tab-size: 2;
		font-family: var(--font-mono);
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
	.row-note {
		flex: 1 1 100%;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.note {
		font-size: var(--text-base);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.segments.services {
		margin-bottom: calc(-1 * var(--space-2));
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.form :global(.line > .field.ro) {
		flex: 0 1 12rem;
	}
	.form :global(.short-name) {
		flex: 0 1 10rem;
	}
	.link {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.link:hover {
		text-decoration: underline;
	}
	/* Always in reach at the bottom of the window while editing. */
	.apply {
		position: sticky;
		bottom: 0;
		z-index: 5;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--surface);
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
	}
	.apply .note {
		flex: 1 1 12rem;
		text-align: right;
	}
	@media (max-width: 760px) {
		.apply .note {
			text-align: left;
		}
	}
</style>
