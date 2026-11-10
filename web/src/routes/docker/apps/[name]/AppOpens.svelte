<script lang="ts">
	// PLAN 72, draft 06: which files the app opens („Öffnen mit“ in the file
	// manager) and which shares are mounted for it. Both change the compose
	// file on the NAS directly; the page reads it again afterwards.
	import { api, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import FormSection from '#lib/components/appform/FormSection.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import {
		appOpens,
		joinCommand,
		mountedShares,
		mountShare,
		setAppOpens,
		splitCommand,
		unmountShare,
		type AppOpens,
	} from '#lib/docker-opens.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		app: string;
		/** The services of the app; the first is the default container. */
		services: string[];
		/** The compose form has unsaved changes: this part waits. */
		locked: boolean;
		/** The compose file changed on the NAS. */
		onchanged: () => void;
	};
	let { app, services, locked, onchanged }: Props = $props();

	let opens = $state<AppOpens | null>(null);
	let loaded = $state(false);
	let editing = $state(false);
	let shown = $state(true);
	let types = $state<string[]>([]);
	let typeInput = $state('');
	let command = $state('');
	let user = $state('');
	let service = $state('');
	let busy = $state(false);
	let error = $state('');
	let shares = $state<Share[]>([]);
	let mountName = $state('');
	let mountRO = $state(false);
	let removing = $state<string | null>(null);

	function fill(o: AppOpens | null) {
		opens = o;
		shown = !o?.hidden;
		types = o?.types ? [...o.types] : [];
		command = o?.command ? joinCommand(o.command) : '';
		user = o?.user ?? '';
		service = o?.service || services[0] || '';
	}

	async function load() {
		fill(await appOpens(app));
		loaded = true;
		editing = !!opens;
	}
	$effect(() => {
		void app;
		void load();
	});
	$effect(() => {
		if (!editing || shares.length) return;
		api.get<Share[]>('/shares').then(
			(list) => (shares = Array.isArray(list) ? list : []),
			() => {},
		);
	});

	const args = $derived(splitCommand(command));
	// Nothing entered yet: no warning about what is missing (the button stays off).
	const started = $derived(types.length > 0 || command.trim() !== '');
	const problem = $derived.by(() => {
		if (!types.length) return t('appopens.needTypes');
		const n = args.filter((a) => a === '{file}').length;
		if (args.length < 2 || n !== 1 || args[0] === '{file}') return t('appopens.needFile');
		return '';
	});
	const mounted = $derived(opens ? mountedShares(opens) : []);
	const free = $derived(
		shares.filter((s) => !s.perUser && !mounted.some((m) => m.share === s.name)),
	);

	function addType() {
		const v = typeInput.trim().toLowerCase().replace(/^\./, '');
		if (/^[a-z0-9]{1,10}$/.test(v) && !types.includes(v)) types = [...types, v];
		typeInput = '';
	}

	async function run(action: () => Promise<unknown>) {
		busy = true;
		error = '';
		try {
			await action();
			onchanged();
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	function save() {
		void run(() =>
			setAppOpens(app, {
				types,
				command: args,
				user: user.trim() || undefined,
				env: opens?.env,
				service,
				hidden: !shown,
			}),
		);
	}
</script>

<FormSection title={t('appopens.title')} />
{#if !loaded}
	<FormRow label={t('appopens.title')}><p class="muted">{t('common.loading')}</p></FormRow>
{:else if !editing}
	<FormRow label={t('appopens.title')} hint={t('appopens.noneHint')}>
		<button type="button" class="btn" disabled={locked} onclick={() => (editing = true)}
			>{t('appopens.setUp')}</button
		>
	</FormRow>
{:else}
	{#if opens?.error}
		<FormRow label={t('appopens.title')} hint={opens.error} tone="error"><span></span></FormRow>
	{/if}
	<FormRow label={t('appopens.shown')} hint={t('appopens.shownHint')}>
		<Toggle bind:checked={shown} label={t('appopens.shown')} disabled={locked || busy} />
	</FormRow>
	<FormRow label={t('appopens.types')} id="opens-type">
		<div class="chips">
			{#each types as type (type)}
				<span class="chip"
					>.{type}<button
						type="button"
						class="x"
						aria-label={t('appopens.removeType', { type })}
						disabled={locked || busy}
						onclick={() => (types = types.filter((x) => x !== type))}>×</button
					></span
				>
			{/each}
			<input
				id="opens-type"
				class="field type"
				placeholder={t('appopens.addType')}
				maxlength="11"
				bind:value={typeInput}
				disabled={locked || busy}
				onkeydown={(e) => {
					if (e.key === 'Enter' || e.key === ',' || e.key === ' ') {
						e.preventDefault();
						addType();
					}
				}}
				onblur={addType}
			/>
		</div>
	</FormRow>
	<FormRow
		label={t('appopens.command')}
		id="opens-command"
		hint={(started && problem) || t('appopens.commandHint')}
		tone={started && problem ? 'warn' : ''}
	>
		<input
			id="opens-command"
			class="field mono"
			spellcheck="false"
			bind:value={command}
			placeholder="bambu-studio {'{file}'}"
			disabled={locked || busy}
		/>
	</FormRow>
	<FormRow label={t('appopens.user')} id="opens-user" hint={t('appopens.userHint')}>
		<input
			id="opens-user"
			class="field mono"
			spellcheck="false"
			bind:value={user}
			disabled={locked || busy}
		/>
	</FormRow>
	{#if services.length > 1}
		<FormRow label={t('appopens.service')} id="opens-service">
			<select id="opens-service" class="field" bind:value={service} disabled={locked || busy}>
				{#each services as s (s)}<option value={s}>{s}</option>{/each}
			</select>
		</FormRow>
	{/if}
	<div class="actions">
		<div class="line">
			<button
				type="button"
				class="btn btn-primary"
				disabled={locked || busy || !!problem}
				onclick={save}>{t('appopens.save')}</button
			>
			{#if !opens}<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => (editing = false)}>{t('common.cancel')}</button
				>{/if}
		</div>
		{#if locked || error}<p class="hint" class:error role={error ? 'alert' : undefined}>
				{locked ? t('appopens.locked') : error}
			</p>{/if}
	</div>

	{#if opens}
		<FormSection title={t('appopens.shares')} />
		{#each mounted as m (m.share)}
			<FormRow
				label={m.share}
				hint={`/shares/${m.share} · ${t(m.readOnly ? 'appopens.readOnly' : 'appopens.readWrite')}`}
			>
				<div class="line">
					<button
						type="button"
						class="btn"
						disabled={locked || busy}
						onclick={() => (removing = m.share)}>{t('appopens.unmount')}</button
					>
				</div>
			</FormRow>
		{:else}
			<FormRow label={t('appopens.shares')} hint={t('appopens.sharesNone')}><span></span></FormRow>
		{/each}
		{#if free.length}
			<FormRow label={t('appopens.mount')} id="opens-share" hint={t('appopens.mountHint')}>
				<div class="line">
					<select id="opens-share" class="field" bind:value={mountName} disabled={locked || busy}>
						<option value="">{t('appopens.pickShare')}</option>
						{#each free as s (s.name)}<option value={s.name}>{s.name}</option>{/each}
					</select>
					<label class="ro"
						><input type="checkbox" bind:checked={mountRO} disabled={locked || busy} />{t(
							'appopens.readOnly',
						)}</label
					>
					<button
						type="button"
						class="btn"
						disabled={locked || busy || !mountName}
						onclick={() => {
							const name = mountName;
							mountName = '';
							void run(() => mountShare(app, name, mountRO));
						}}>{t('appopens.mountButton')}</button
					>
				</div>
			</FormRow>
		{/if}
	{/if}
{/if}

{#if removing}
	<Dialog
		title={t('appopens.unmountTitle', { share: removing })}
		locked={busy}
		onclose={() => (removing = null)}
	>
		<p>{t('appopens.unmountText', { share: removing })}</p>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn btn-warn"
				disabled={busy}
				onclick={() => {
					const share = removing!;
					removing = null;
					void run(() => unmountShare(app, share));
				}}>{t('appopens.unmount')}</button
			>
			<!-- svelte-ignore a11y_autofocus -->
			<button type="button" class="btn" autofocus onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chips,
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5);
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		padding: 2px var(--space-1) 2px var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: 999px;
		font-size: var(--text-sm);
	}
	.x {
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
		padding: 0 var(--space-1);
		font-size: 1rem;
		line-height: 1;
	}
	.field.type {
		width: 9rem;
	}
	.actions {
		display: grid;
		grid-template-columns: minmax(10rem, 17rem) minmax(0, 1fr);
		gap: 0 var(--space-4);
		padding: 7px var(--space-4);
	}
	.actions > * {
		grid-column: 2;
	}
	@media (max-width: 760px) {
		.actions {
			grid-template-columns: minmax(0, 1fr);
			padding: var(--space-1-5) var(--space-3);
		}
		.actions > * {
			grid-column: 1;
		}
	}
	.actions .hint {
		margin: var(--space-1-5) 0 0;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.actions .hint.error {
		color: var(--error-text);
	}
	.ro {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		font-size: var(--text-sm);
	}
	p {
		margin: 0 0 var(--space-3);
	}
</style>
