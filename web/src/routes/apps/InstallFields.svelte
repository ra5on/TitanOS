<script lang="ts">
	// The lines of the install form: images, access, devices, paths, ports and
	// variables; advanced lines on demand.
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import CpuPicker from '#lib/components/appform/CpuPicker.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import MemoryLimit from '#lib/components/appform/MemoryLimit.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import PathField from '#lib/components/appform/PathField.svelte';
	import SecretField from '#lib/components/appform/SecretField.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import { where } from '#lib/components/appform/where.ts';
	import { validImage } from '#lib/compose/image.ts';
	import { localize, type InstallForm, type Template } from '#lib/compose/template.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import type { Snippet } from 'svelte';
	import type { Installer } from './install-form.svelte.ts';

	let {
		ins,
		form = $bindable(),
		problems,
		tpl,
		head,
	}: {
		ins: Installer;
		form: InstallForm;
		problems: NonNullable<Installer['problems']>;
		tpl: Template;
		/** Rows of the page that stand first in the first group (name, description). */
		head?: Snippet;
	} = $props();
</script>

<!-- PLAN 78-5: the form's sections are plain headings above flat rows. -->
<SettingGroup>
	{@render head?.()}
	{#each form.images as field, index (field.service)}
		{#if index === ins.mainImage || ins.advanced}
			<FormRow
				label={form.images.length > 1
					? t('appform.repositoryOf', { service: field.service })
					: t('appform.repository')}
				id="image-{index}"
				hint={!validImage(field.image)
					? t('editor.imageInvalid')
					: index === ins.mainImage && form.images.length > 1 && !ins.advanced
						? t('appform.moreImages', { count: form.images.length - 1 })
						: ''}
				tone={!validImage(field.image) ? 'error' : ''}
			>
				<div class="line">
					<input
						id="image-{index}"
						class="field mono"
						bind:value={field.image}
						placeholder="ghcr.io/…:latest"
						spellcheck="false"
						aria-invalid={!validImage(field.image)}
					/>
				</div>
			</FormRow>
		{/if}
	{/each}
	{#if ins.advanced}
		<FormRow label={t('editor.publicUrl')} id="public-url" hint={t('editor.publicUrlHint')}>
			<div class="line">
				<input
					id="public-url"
					class="field mono"
					type="url"
					bind:value={form.publicUrl}
					placeholder="https://app.example.org"
					spellcheck="false"
				/>
			</div>
		</FormRow>
		<FormRow
			wide
			label={t('appform.cpus')}
			hint={problems.cpuset ? t('appform.cpusInvalid') : t('appform.cpusHint')}
			tone={problems.cpuset ? 'error' : ''}
		>
			<CpuPicker bind:cpuset={form.cpuset} threads={ins.threads} />
		</FormRow>
		<FormRow
			label={t('appform.memory')}
			id="memory"
			hint={problems.memLimit ? t('appform.memoryInvalid') : t('appform.memoryHint')}
			tone={problems.memLimit ? 'error' : ''}
		>
			<MemoryLimit id="memory" bind:value={form.memLimit} />
		</FormRow>
	{/if}
</SettingGroup>

{#if ins.offerHttps}
	<SettingGroup title={t('appform.sectionAccess')} id="app-access">
		<FormRow
			label={t('appform.httpsName')}
			id="https-name"
			hint={ins.httpsProblem ||
				(ins.https.enabled ? t('appform.httpsHint') : t('appform.httpsOffHint'))}
			tone={ins.httpsProblem ? 'error' : ins.https.enabled ? 'ok' : ''}
		>
			<div class="line">
				<Toggle bind:checked={ins.https.enabled} label={t('appform.httpsName')} text="" />
				{#if ins.https.enabled}<input
						id="https-name"
						class="field mono short-name"
						bind:value={ins.https.name}
						oninput={() => (ins.httpsTouched = true)}
						spellcheck="false"
						autocapitalize="none"
					/><span class="muted mono">.{ins.proxy?.suffixes[0] ?? ''}</span>{/if}
			</div>
		</FormRow>
	</SettingGroup>
{/if}

{#if Object.keys(ins.selectedHardware).length}
	<SettingGroup title={t('appform.sectionDevices')} id="app-devices">
		{#each ins.gpuServices as service (service)}
			<FormRow
				wide
				label={ins.advanced && Object.keys(ins.selectedHardware).length > 1
					? t('appform.gpuFor', { service })
					: t('appform.gpu')}
				hint={t('hardware.containerHint')}
			>
				<HardwarePicker bind:selected={ins.selectedHardware[service]} />
			</FormRow>
		{/each}
	</SettingGroup>
{/if}

{#if form.paths.length || form.ports.length || form.vars.length || form.own.paths.length || form.own.ports.length || form.own.vars.length || form.own.labels.length || form.own.devices.length}
	<SettingGroup title={t('appform.sectionEntries')} id="app-entries">
		{#each form.paths as field, index (field.service + field.index)}
			{@const label = localize(field.label, i18n.locale) || field.container}
			{@const hint = ins.pathHint(field, problems.paths[index])}
			<FormRow {label} id="path-{index}" hint={hint.text} tone={hint.tone}>
				<PathField
					id="path-{index}"
					{label}
					bind:value={field.host}
					shares={ins.shareRefs}
					invalid={problems.paths[index] === 'invalid'}
					oninput={() => ins.editPath(field)}
				/>
			</FormRow>
		{/each}
		{#each form.own.paths as row, index (index)}
			{@const problem = problems.ownPaths[index]}
			<FormRow
				label={row.container || t('apps.ownPath')}
				hint={problem === 'invalid'
					? t('apps.pathInvalid')
					: [
							t('appform.containerPathHint', { path: row.container }),
							where(row.host),
							ins.origin('own'),
						]
							.filter(Boolean)
							.join(' · ')}
				tone={problem === 'invalid' ? 'error' : problem ? 'warn' : ''}
			>
				<PathField label={row.container} bind:value={row.host} shares={ins.shareRefs}>
					{#snippet extra()}<button
							type="button"
							class="btn"
							onclick={() => form!.own.paths.splice(index, 1)}>{t('apps.remove')}</button
						>{/snippet}
				</PathField>
			</FormRow>
		{/each}
		{#each form.ports as field, index (field.service + field.index)}
			{@const label =
				localize(field.label, i18n.locale) || t('apps.portDefault', { port: field.container })}
			{@const hint = ins.portHint(field, problems.ports[index])}
			<FormRow {label} id="port-{index}" hint={hint.text} tone={hint.tone}>
				<div class="line">
					<input
						id="port-{index}"
						class="field mono short"
						inputmode="numeric"
						bind:value={field.host}
						aria-invalid={problems.ports[index] !== ''}
					/>
					{#if problems.ports[index] === 'inUse' || problems.ports[index] === 'duplicate'}
						{@const free = ins.freePort(
							Number(field.host) || Number(field.container),
							field.protocol,
						)}
						<button type="button" class="btn" onclick={() => (field.host = String(free))}
							>{t('appform.takePort', { port: free })}</button
						>
					{/if}
				</div>
			</FormRow>
		{/each}
		{#each form.own.ports as row, index (index)}
			{@const problem = problems.ownPorts[index]}
			<FormRow
				label={t('apps.portDefault', { port: row.container || '?' })}
				hint={problem === 'invalid'
					? t('apps.portInvalid')
					: problem === 'duplicate'
						? t('apps.portDuplicate')
						: problem === 'inUse'
							? t('apps.portInUse', {
									by: ins.by(ins.used.get(`${Number(row.host)}/${row.protocol}`)),
								})
							: `${t('appform.containerPortHint', { port: `${row.container}/${row.protocol}` })} · ${ins.origin('own')}`}
				tone={problem && problem !== 'empty' ? 'warn' : ''}
			>
				<div class="line">
					<input
						class="field mono short"
						inputmode="numeric"
						bind:value={row.host}
						aria-label={t('apps.ownPortHost')}
					/>
					<button type="button" class="btn" onclick={() => form!.own.ports.splice(index, 1)}
						>{t('apps.remove')}</button
					>
				</div>
			</FormRow>
		{/each}
		{#each form.vars as field, index (field.service + field.name)}
			{@const label = localize(field.label, i18n.locale) || field.name}
			<FormRow
				{label}
				id="var-{index}"
				hint={problems.vars[index] ? t('appform.required') : ins.varHint(field)}
				tone={problems.vars[index] ? 'error' : ''}
			>
				{#if field.secret}
					<SecretField
						id="var-{index}"
						bind:value={field.value}
						placeholder={field.placeholder}
						renew={field.origin === 'generated'}
						invalid={problems.vars[index] !== ''}
					/>
				{:else}
					<div class="line">
						<input
							id="var-{index}"
							class="field mono"
							bind:value={field.value}
							placeholder={field.placeholder}
							autocomplete="off"
							spellcheck="false"
							aria-invalid={problems.vars[index] !== ''}
						/>
					</div>
				{/if}
			</FormRow>
		{/each}
		{#each form.own.vars as pair, index (index)}
			{@const problem = problems.ownVars[index]}
			<FormRow
				label={pair.name || t('apps.ownVar')}
				hint={problem === 'invalid'
					? t('apps.ownVarInvalid')
					: problem === 'duplicate'
						? t('apps.ownVarDuplicate')
						: problem === 'replaces'
							? t('apps.ownVarReplaces')
							: `${t('appform.variable', { name: pair.name })} · ${ins.origin('own')}`}
				tone={problem && problem !== 'replaces' ? 'error' : ''}
			>
				<div class="line">
					<input
						class="field mono"
						bind:value={pair.value}
						aria-label={t('apps.ownVarValue')}
						spellcheck="false"
					/>
					<button type="button" class="btn" onclick={() => form!.own.vars.splice(index, 1)}
						>{t('apps.remove')}</button
					>
				</div>
			</FormRow>
		{/each}
		{#each form.own.labels as pair, index (index)}
			<FormRow label={pair.name} hint={`${t('appform.label')} · ${ins.origin('own')}`}>
				<div class="line">
					<input
						class="field mono"
						bind:value={pair.value}
						aria-label={pair.name}
						spellcheck="false"
					/>
					<button type="button" class="btn" onclick={() => form!.own.labels.splice(index, 1)}
						>{t('apps.remove')}</button
					>
				</div>
			</FormRow>
		{/each}
		{#each form.own.devices as _, index (index)}
			<FormRow
				label={t('appform.device')}
				hint={problems.ownDevices[index] ? t('appform.addDeviceHint') : ins.origin('own')}
				tone={problems.ownDevices[index] ? 'error' : ''}
			>
				<div class="line">
					<input
						class="field mono"
						bind:value={form.own.devices[index]}
						aria-label={t('appform.device')}
						spellcheck="false"
					/>
					<button type="button" class="btn" onclick={() => form!.own.devices.splice(index, 1)}
						>{t('apps.remove')}</button
					>
				</div>
			</FormRow>
		{/each}
	</SettingGroup>
{/if}

<div class="links">
	<button type="button" class="link" onclick={() => (ins.advanced = !ins.advanced)}
		>{ins.advanced ? t('appform.fewer') : t('appform.more', { count: ins.hiddenCount })}</button
	>
	<button type="button" class="link add" onclick={() => (ins.adding = true)}
		><Icon name="plus" size={14} />{t('appform.addEntry')}</button
	>
</div>

<style>
	.short-name {
		flex: 0 1 12rem !important;
	}
	.links {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
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
</style>
