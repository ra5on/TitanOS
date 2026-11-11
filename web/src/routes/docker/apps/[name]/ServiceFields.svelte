<script lang="ts">
	// The form of one service of the app: image, restart, access, devices and
	// the entries (paths, ports, variables, labels); every change is written
	// into the compose text.
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import LANAssignment from '#lib/components/LANAssignment.svelte';
	import CpuPicker from '#lib/components/appform/CpuPicker.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import MemoryLimit from '#lib/components/appform/MemoryLimit.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import PathField from '#lib/components/appform/PathField.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import { where } from '#lib/components/appform/where.ts';
	import { validImage } from '#lib/compose/image.ts';
	import { validCpuset, validDevice, validMemLimit } from '#lib/compose/template.ts';
	import { NETWORKS, RESTART_POLICIES, type ServiceForm } from '#lib/compose/service.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { publicURL } from '#lib/app-links.ts';
	import { system } from '#lib/system.svelte.ts';
	import AppAccess from './AppAccess.svelte';
	import type { Snippet } from 'svelte';
	import type { AppEditor } from './app-editor.svelte.ts';
	import AppIpv64 from './AppIpv64.svelte';
	import AppOpens from './AppOpens.svelte';
	import AppProxy from './AppProxy.svelte';

	let {
		ed,
		form = $bindable(),
		problems,
		head,
	}: {
		ed: AppEditor;
		form: ServiceForm;
		problems: NonNullable<AppEditor['problems']>;
		/** Rows of the page that stand first in the first group (the app's name). */
		head?: Snippet;
	} = $props();
</script>

<!-- PLAN 78-5: the form's sections are plain headings above flat rows. -->
<SettingGroup>
	{@render head?.()}
	<FormRow
		label={t('appform.repository')}
		id="image"
		changed={ed.differs('image')}
		hint={!validImage(form.image)
			? t('editor.imageInvalid')
			: ed.updateAvailable
				? t('appform.updateAvailable')
				: ''}
		tone={!validImage(form.image) ? 'error' : ed.updateAvailable ? 'warn' : ''}
	>
		<div class="line">
			<input
				id="image"
				class="field mono"
				bind:value={form.image}
				oninput={ed.commit}
				spellcheck="false"
				aria-invalid={!validImage(form.image)}
			/>
		</div>
	</FormRow>
	<FormRow
		label={t('editor.webui')}
		id="webui"
		changed={ed.differs('webui')}
		hint={t('editor.webuiHint')}
	>
		<div class="line">
			<input
				id="webui"
				class="field mono"
				bind:value={form.webui}
				oninput={ed.commit}
				placeholder="8096"
				spellcheck="false"
			/>
		</div>
	</FormRow>
	<FormRow label={t('editor.restart')} id="restart" changed={ed.differs('restart')}>
		<div class="line">
			<select id="restart" class="field" bind:value={form.restart} onchange={ed.commit}
				>{#each RESTART_POLICIES as policy (policy)}<option value={policy}
						>{t(`editor.restart.${policy}` as 'editor.restart.no')}</option
					>{/each}{#if !RESTART_POLICIES.includes(form.restart)}<option value={form.restart}
						>{form.restart}</option
					>{/if}</select
			>
		</div>
	</FormRow>
	{#if ed.advanced}
		<FormRow
			label={t('editor.publicUrl')}
			id="public-url"
			changed={ed.differs('publicUrl')}
			hint={form.publicUrl.trim() && !publicURL(form.publicUrl)
				? t('editor.publicUrlInvalid')
				: t('editor.publicUrlHint')}
			tone={form.publicUrl.trim() && !publicURL(form.publicUrl) ? 'error' : ''}
		>
			<div class="line">
				<input
					id="public-url"
					class="field mono"
					type="url"
					bind:value={form.publicUrl}
					oninput={ed.commit}
					placeholder="https://app.example.org"
					spellcheck="false"
				/>
			</div>
		</FormRow>
		<FormRow
			wide
			label={t('appform.cpus')}
			changed={ed.differs('cpuset')}
			hint={validCpuset(form.cpuset) ? t('appform.cpusHint') : t('appform.cpusInvalid')}
			tone={validCpuset(form.cpuset) ? '' : 'error'}
		>
			<CpuPicker
				bind:cpuset={form.cpuset}
				threads={system.info?.cpu.threads ?? 0}
				onchange={ed.commit}
			/>
		</FormRow>
		<FormRow
			label={t('appform.memory')}
			id="memory"
			changed={ed.differs('memLimit')}
			hint={validMemLimit(form.memLimit) ? t('appform.memoryHint') : t('appform.memoryInvalid')}
			tone={validMemLimit(form.memLimit) ? '' : 'error'}
		>
			<MemoryLimit id="memory" bind:value={form.memLimit} onchange={ed.commit} />
		</FormRow>
	{/if}
</SettingGroup>

<SettingGroup title={t('appform.sectionAccess')} id="app-access">
	<FormRow
		label={t('editor.network')}
		id="network"
		changed={ed.differs('network')}
		hint={t('appform.networkHint')}
	>
		<div class="line">
			<select id="network" class="field" bind:value={form.network} onchange={ed.commit}
				>{#each NETWORKS as mode (mode)}<option value={mode}
						>{t(`editor.network.${mode}` as 'editor.network.host')}</option
					>{/each}{#if !NETWORKS.includes(form.network)}<option value={form.network}
						>{form.network}</option
					>{/if}</select
			>
		</div>
	</FormRow>
	{#if form.network !== 'host' && form.network !== 'none'}
		<FormRow wide label={t('lan.title')} hint={t('lan.service')}>
			<LANAssignment
				text={ed.text}
				service={ed.service}
				onchange={(next) => {
					ed.text = next;
					ed.parse();
				}}
			/>
		</FormRow>
	{/if}
	<FormRow wide label={t('appform.httpsName')}>
		<AppProxy app={ed.name} members={ed.members} version={ed.proxyVersion} />
	</FormRow>
	<FormRow wide label={t('appaccess.whoMay')}>
		<AppAccess app={ed.name} />
	</FormRow>
	{#if ed.proxy}
		<FormRow
			label={t('appform.tailscale')}
			hint={ed.published ? t('appform.tailnetOnHint') : t('appform.tailnetOffHint')}
			tone={ed.published ? 'ok' : ''}
		>
			<div class="line">
				<span class="state-text">{t(ed.published ? 'appform.on' : 'appform.off')}</span>
				<button
					type="button"
					class="btn"
					onclick={() => (ed.tailnetDialog = ed.published ? 'remove' : 'publish')}
					><Icon name="tailnet" size={15} />{t(
						ed.published ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish',
					)}</button
				>
			</div>
		</FormRow>
	{/if}
	<FormRow wide label={t('appform.ipv64')}>
		<AppIpv64 app={ed.name} />
	</FormRow>
	<FormRow
		label={t('editor.privileged')}
		changed={ed.differs('privileged')}
		hint={form.privileged ? t('editor.privilegedWarning') : ''}
		tone={form.privileged ? 'warn' : ''}
	>
		<Toggle bind:checked={form.privileged} label={t('editor.privileged')} onchange={ed.commit} />
	</FormRow>
</SettingGroup>

<AppOpens
	app={ed.name}
	services={ed.services}
	locked={ed.changed}
	onchanged={() => ed.loadCompose()}
/>

<SettingGroup title={t('appform.sectionDevices')} id="app-devices">
	<FormRow
		wide
		label={t('appform.gpu')}
		changed={ed.differs('hardware')}
		hint={t('hardware.containerHint')}
	>
		<HardwarePicker bind:selected={form.hardware} onchange={ed.commit} />
	</FormRow>
	{#each form.devices as row, index (index)}
		{#if row.raw !== undefined}
			<FormRow label={t('appform.device')} hint={t('editor.rawEntry')}>
				<p class="text mono">{JSON.stringify(row.raw)}</p>
			</FormRow>
		{:else}
			<FormRow
				label={t('appform.device')}
				changed={ed.differs('devices')}
				hint={validDevice(row.value) ? '' : t('appform.addDeviceHint')}
				tone={validDevice(row.value) ? '' : 'error'}
			>
				<div class="line">
					<input
						class="field mono"
						bind:value={row.value}
						oninput={ed.commit}
						aria-label={t('appform.device')}
						spellcheck="false"
					/>
					{#if ed.advanced}<button
							type="button"
							class="btn"
							onclick={() => (form!.devices.splice(index, 1), ed.commit())}
							>{t('editor.remove')}</button
						>{/if}
				</div>
			</FormRow>
		{/if}
	{/each}
</SettingGroup>

<SettingGroup title={t('appform.sectionEntries')} id="app-entries">
	{#each form.paths as row, index (index)}
		{#if row.raw !== undefined}
			<FormRow label={t('editor.paths')} hint={t('editor.rawEntry')}>
				<p class="text mono">
					{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}
				</p>
			</FormRow>
		{:else}
			<FormRow
				label={row.container || t('apps.ownPath')}
				changed={ed.rowChanged('paths', index)}
				hint={problems.paths[index] ||
					[
						t('appform.containerPathHint', { path: row.container }),
						where(row.host),
						row.readOnly ? t('editor.readOnly') : '',
					]
						.filter(Boolean)
						.join(' · ')}
				tone={problems.paths[index] ? 'error' : ''}
			>
				<PathField label={row.container} bind:value={row.host} oninput={ed.commit} />
				{#if ed.advanced}
					<div class="line">
						<input
							class="field mono"
							bind:value={row.container}
							oninput={ed.commit}
							aria-label={t('editor.containerPath')}
							spellcheck="false"
						/>
						<select
							class="field ro"
							value={row.readOnly ? 'ro' : 'rw'}
							aria-label={t('editor.readOnly')}
							onchange={(event) => (
								(row.readOnly = event.currentTarget.value === 'ro'),
								ed.commit()
							)}
							><option value="rw">{t('editor.readWrite')}</option><option value="ro"
								>{t('editor.readOnly')}</option
							></select
						>
						<button
							type="button"
							class="btn"
							onclick={() => (form!.paths.splice(index, 1), ed.commit())}
							>{t('editor.remove')}</button
						>
					</div>
				{/if}
			</FormRow>
		{/if}
	{/each}
	{#if form.network !== 'host' && form.network !== 'none'}
		{#each form.ports as row, index (index)}
			{#if row.raw !== undefined}
				<FormRow label={t('editor.ports')} hint={t('editor.rawEntry')}>
					<p class="text mono">
						{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}
					</p>
				</FormRow>
			{:else}
				<FormRow
					label={t('apps.portDefault', { port: row.container || '?' })}
					changed={ed.rowChanged('ports', index)}
					hint={problems.ports[index] ||
						t('appform.containerPortHint', { port: `${row.container}/${row.protocol}` })}
					tone={problems.ports[index] ? 'error' : ''}
				>
					<div class="line">
						<input
							class="field mono short"
							inputmode="numeric"
							bind:value={row.host}
							oninput={ed.commit}
							aria-label={t('editor.hostPort')}
						/>
						{#if ed.advanced}
							<input
								class="field mono short"
								inputmode="numeric"
								bind:value={row.container}
								oninput={ed.commit}
								aria-label={t('editor.containerPort')}
							/>
							<select
								class="field short"
								bind:value={row.protocol}
								onchange={ed.commit}
								aria-label={t('apps.protocol')}
								><option value="tcp">TCP</option><option value="udp">UDP</option></select
							>
							<button
								type="button"
								class="btn"
								onclick={() => (form!.ports.splice(index, 1), ed.commit())}
								>{t('editor.remove')}</button
							>
						{/if}
					</div>
				</FormRow>
			{/if}
		{/each}
	{/if}
	{#each form.env as pair, index (index)}
		<FormRow
			label={pair.name || t('apps.ownVar')}
			changed={ed.envChanged(pair)}
			hint={problems.env[index] || t('appform.variable', { name: pair.name })}
			tone={problems.env[index] ? 'error' : ''}
		>
			<div class="line">
				{#if ed.advanced}<input
						class="field mono short-name"
						bind:value={pair.name}
						oninput={ed.commit}
						aria-label={t('editor.envName')}
						spellcheck="false"
					/>{/if}
				<input
					class="field mono"
					bind:value={pair.value}
					oninput={ed.commit}
					aria-label={pair.name}
					spellcheck="false"
				/>
				{#if ed.advanced}<button
						type="button"
						class="btn"
						onclick={() => (form!.env.splice(index, 1), ed.commit())}>{t('editor.remove')}</button
					>{/if}
			</div>
		</FormRow>
	{/each}
	{#if ed.advanced}
		{#each form.labels as pair, index (index)}
			<FormRow
				label={pair.name || t('appform.label')}
				changed={ed.differs('labels')}
				hint={t('appform.label')}
			>
				<div class="line">
					<input
						class="field mono short-name"
						bind:value={pair.name}
						oninput={ed.commit}
						aria-label={t('editor.envName')}
						spellcheck="false"
					/>
					<input
						class="field mono"
						bind:value={pair.value}
						oninput={ed.commit}
						aria-label={t('editor.envValue')}
						spellcheck="false"
					/>
					<button
						type="button"
						class="btn"
						onclick={() => (form!.labels.splice(index, 1), ed.commit())}
						>{t('editor.remove')}</button
					>
				</div>
			</FormRow>
		{/each}
	{/if}
</SettingGroup>
<div class="links">
	<button type="button" class="link" onclick={() => (ed.advanced = !ed.advanced)}
		>{ed.advanced ? t('appform.fewer') : t('appform.more', { count: ed.hiddenCount })}</button
	>
	<button type="button" class="link" onclick={() => (ed.adding = true)}
		><Icon name="plus" size={14} />{t('appform.addEntry')}</button
	>
</div>

<style>
	.state-text {
		min-width: 3rem;
		font-size: var(--text-md);
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
