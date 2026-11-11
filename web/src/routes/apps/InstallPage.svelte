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
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
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
	{#snippet headRows()}
		<!-- A snippet does not inherit the checks of the branch it stands in. -->
		{@const form = ins.form!}
		{@const problems = ins.problems!}
		{@const tpl = ins.tpl!}
		{#if !ins.own && ins.app}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={problems.name
					? t(problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: tpl.appdata
						? t('apps.nameHint', { path: `${tpl.appdata}/${form.name}` })
						: ''}
				tone={problems.name ? 'error' : ''}
			>
				<div class="line">
					<span class="icon"><AppIcon src={ins.app.icon} name={ins.app.name} /></span>
					<input
						id="app-name"
						class="field mono"
						bind:value={form.name}
						oninput={ins.renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={problems.name !== ''}
					/>
				</div>
			</FormRow>
			<FormRow
				label={t('appform.description')}
				hint="{localize(ins.app.description, i18n.locale) || ins.app.image} · {t(
					'apps.panelSource',
					{ source: ins.sourceName },
				)}"
			/>
		{:else}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={problems.name
					? t(problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: t('appform.ownHint')}
				tone={problems.name ? 'error' : ''}
			>
				<div class="line">
					<input
						id="app-name"
						class="field mono"
						bind:value={form.name}
						oninput={ins.renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={problems.name !== ''}
					/>
				</div>
			</FormRow>
		{/if}
		{#if tpl.cpuMissing?.length || form.risks.length || form.missingDevices.length || localize(form.tips, i18n.locale)}
			<FormRow wide label={t('appform.requirements')}>
				<div class="notes">
					{#if tpl.cpuMissing?.length}<p class="notice-warn" role="note">
							{t('apps.cpuMissing', { list: needList(tpl.cpuMissing) })}
						</p>{/if}
					{#if form.risks.length}<div class="notice-warn" role="note">
							{t('apps.risks')}
							<ul>
								{#each form.risks as risk (risk)}<li>{t(`apps.risk.${risk}`)}</li>{/each}
							</ul>
						</div>{/if}
					{#if form.missingDevices.length}<p class="muted text">
							{t('apps.missingDevices', { devices: form.missingDevices.join(', ') })}
						</p>{/if}
					{#if localize(form.tips, i18n.locale)}<details>
							<summary>{t('apps.tips')}</summary>
							<pre class="tips">{localize(form.tips, i18n.locale)}</pre>
						</details>{/if}
				</div>
			</FormRow>
		{/if}
	{/snippet}
	<!-- PLAN 78-5: the form is no tile: groups of flat rows under plain
	     headings, the buttons below them. -->
	<section class="form">
		{#if ins.view === 'compose'}
			<SettingGroup>
				{@render headRows()}
				<FormRow wide label={t('appform.composeFile')} hint={t('apps.composeNote')}>
					<textarea
						class="field mono yaml"
						bind:value={ins.text}
						spellcheck="false"
						aria-label={t('apps.asCompose')}></textarea>
				</FormRow>
			</SettingGroup>
		{:else}
			<InstallFields
				{ins}
				bind:form={ins.form}
				problems={ins.problems}
				tpl={ins.tpl}
				head={headRows}
			/>
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
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
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
		margin: 0;
	}
	.error pre {
		white-space: pre-wrap;
	}
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.spacer {
		flex: 1;
	}
</style>
