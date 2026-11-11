<script lang="ts">
	// Installs an app: a template of the catalog, or a blank one for an own
	// container. One column of lines like the app editor (decided 1.11.2026,
	// PLAN 21): name, repository, access, devices, paths, ports and
	// variables; advanced lines on demand. "Install" then shows the log of
	// the job until the app runs.
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import AddEntryDialog from '#lib/components/appform/AddEntryDialog.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import ViewSwitch from '#lib/components/appform/ViewSwitch.svelte';
	import { localize } from '#lib/compose/template.ts';
	import { needList } from '#lib/hwcheck.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import InstallFields from './InstallFields.svelte';
	import { Installer } from './install-form.svelte.ts';
	import InstallProgress from './InstallProgress.svelte';

	/** The app of the catalog; without it an own container is set up. */
	let { source = '', id = '' }: { source?: string; id?: string } = $props();

	// State and actions live in install-form.svelte.ts.
	const ins = new Installer(
		() => source,
		() => id,
	);
</script>

<PageHead title={ins.title}>
	{#snippet actions()}
		{#if ins.phase === 'form' && ins.form}<ViewSwitch bind:advanced={ins.advanced} />{/if}
	{/snippet}
</PageHead>

{#if ins.loadError}
	<div class="notice-error" role="alert">{ins.loadError}</div>
{:else if !ins.form || !ins.problems || !ins.tpl}
	<p class="muted" role="status">{t('apps.loading')}</p>
{:else if ins.phase !== 'form'}
	<InstallProgress {ins} form={ins.form} />
{:else}
	{#if !ins.ready && ins.docker}<p class="notice-warn" role="status">{t('apps.dockerOff')}</p>{/if}
	<section class="tile form">
		{#if !ins.own && ins.app}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={ins.problems.name
					? t(ins.problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: ins.tpl.appdata
						? t('apps.nameHint', { path: `${ins.tpl.appdata}/${ins.form.name}` })
						: ''}
				tone={ins.problems.name ? 'error' : ''}
			>
				<div class="line">
					<span class="icon"><AppIcon src={ins.app.icon} name={ins.app.name} /></span>
					<input
						id="app-name"
						class="field mono"
						bind:value={ins.form.name}
						oninput={ins.renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={ins.problems.name !== ''}
					/>
				</div>
			</FormRow>
			<FormRow label={t('appform.description')}>
				<p class="text">
					{localize(ins.app.description, i18n.locale) || ins.app.image}
					<span class="muted">· {t('apps.panelSource', { source: ins.sourceName })}</span>
				</p>
			</FormRow>
		{:else}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={ins.problems.name
					? t(ins.problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: t('appform.ownHint')}
				tone={ins.problems.name ? 'error' : ''}
			>
				<div class="line">
					<input
						id="app-name"
						class="field mono"
						bind:value={ins.form.name}
						oninput={ins.renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={ins.problems.name !== ''}
					/>
				</div>
			</FormRow>
		{/if}
		{#if ins.tpl.cpuMissing?.length || ins.form.risks.length || ins.form.missingDevices.length || localize(ins.form.tips, i18n.locale)}
			<FormRow label={t('appform.requirements')}>
				<div class="notes">
					{#if ins.tpl.cpuMissing?.length}<p class="notice-warn" role="note">
							{t('apps.cpuMissing', { list: needList(ins.tpl.cpuMissing) })}
						</p>{/if}
					{#if ins.form.risks.length}<div class="notice-warn" role="note">
							{t('apps.risks')}
							<ul>
								{#each ins.form.risks as risk (risk)}<li>{t(`apps.risk.${risk}`)}</li>{/each}
							</ul>
						</div>{/if}
					{#if ins.form.missingDevices.length}<p class="muted text">
							{t('apps.missingDevices', { devices: ins.form.missingDevices.join(', ') })}
						</p>{/if}
					{#if localize(ins.form.tips, i18n.locale)}<details>
							<summary>{t('apps.tips')}</summary>
							<pre class="tips">{localize(ins.form.tips, i18n.locale)}</pre>
						</details>{/if}
				</div>
			</FormRow>
		{/if}

		{#if ins.view === 'compose'}
			<FormRow label={t('appform.composeFile')} hint={t('apps.composeNote')}>
				<textarea
					class="field mono yaml"
					bind:value={ins.text}
					spellcheck="false"
					aria-label={t('apps.asCompose')}></textarea>
			</FormRow>
		{:else}
			<InstallFields {ins} bind:form={ins.form} problems={ins.problems} tpl={ins.tpl} />
		{/if}

		{#if ins.error}
			<div class="notice-error error" role="alert">
				{ins.error.text}{#if ins.error.detail}<pre>{ins.error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="foot">
			<button
				type="button"
				class="btn btn-primary"
				onclick={ins.install}
				disabled={!ins.ready ||
					(ins.view === 'form' &&
						(ins.problems.blocking || ins.imageProblem || !!ins.httpsProblem))}
				>{t('apps.install')}</button
			>
			<a class="btn" href="/apps">{t('common.cancel')}</a>
			<span class="spacer"></span>
			<button
				type="button"
				class="link"
				onclick={() => ins.show(ins.view === 'form' ? 'compose' : 'form')}
				>{t(ins.view === 'form' ? 'appform.editCompose' : 'apps.asForm')}</button
			>
		</div>
	</section>
{/if}

{#if ins.adding}<AddEntryDialog
		shares={ins.shareRefs}
		onadd={ins.addEntry}
		onclose={() => (ins.adding = false)}
	/>{/if}

{#if ins.leaving}
	<Dialog title={t('apps.discardTitle')} onclose={() => (ins.leaving = null)}>
		<p>{t('apps.discardText', { name: ins.form?.name ?? '' })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ins.leaving = null)}
				>{t('shares.keepEditing')}</button
			>
			<button type="button" class="btn btn-warn" onclick={() => ins.leave(ins.leaving ?? '/apps')}
				>{t('shares.discard')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.form {
		padding: var(--space-2) 0 0;
	}
	.icon {
		display: inline-flex;
		width: 2.25rem;
		height: 2.25rem;
		flex: none;
	}
	.notes {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-width: 46rem;
		padding-top: var(--space-1);
		font-size: var(--text-md);
	}
	.notes p,
	.notes ul {
		margin: 0;
	}
	.tips {
		white-space: pre-wrap;
		font-size: var(--text-sm);
	}
	.yaml {
		min-height: 24rem;
		max-width: 100%;
		font-size: var(--text-sm);
		tab-size: 2;
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
	.error {
		margin: var(--space-2) var(--space-4);
	}
	.error pre {
		white-space: pre-wrap;
	}
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-4) calc(min(17rem, 30%) + 2 * var(--space-4));
		border-top: 1px solid var(--border);
	}
	.spacer {
		flex: 1;
	}
	@media (max-width: 760px) {
		.foot {
			padding-left: var(--space-3);
		}
	}
</style>
