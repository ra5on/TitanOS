<script lang="ts">
	import { untrack } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import { api, ApiError, type AccessLevel, type Account, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { radioKeys } from '#lib/radio-group.ts';

	type Props = {
		/** The share to edit; undefined creates a new one. */
		share?: Share;
		volumes: string[];
		accounts: Account[];
		onclose: () => void;
		ondone: () => void;
	};
	let { share, volumes, accounts, onclose, ondone }: Props = $props();

	// As shares.ValidName: letters, digits, blanks, - _ and ., up to 32 characters,
	// no blank or dot at the start or end, no double blank, no name Samba reserves.
	const formID = $props.id();
	const NAME = /^(?![ .])(?!.*[ .]$)(?!.*  )[\p{L}\p{N} ._-]{1,32}$/u;
	const RESERVED = ['global', 'homes', 'printers', 'print', 'ipc'];
	const validName = (name: string) => NAME.test(name) && !RESERVED.includes(name.toLowerCase());
	const LEVELS: ('' | AccessLevel)[] = ['', 'ro', 'rw'];

	// Props are read once on purpose: the draft starts from them and later
	// changes of the props must not overwrite what is being edited; the shares page mounts this dialog anew for each opening ({#if}/{#key dialog}).
	// svelte-ignore state_referenced_locally
	let draft = $state({
		name: share?.name ?? '',
		volume: share?.volume ?? volumes[0] ?? '',
		smb: share?.smb ?? true,
		nfs: (share?.nfs.enabled ? share.nfs.mode || 'rw' : 'off') as 'off' | 'ro' | 'rw',
		clients: (share?.nfs.clients ?? []).join('\n'),
		// Whoever creates a share can use it; Samba does not offer a share no one may open.
		access: {
			...(share?.access ?? (session.data?.user ? { [session.data.user.name]: 'rw' } : {})),
		} as Record<string, AccessLevel>,
		guest: share?.guest ?? false,
		browseable: share?.browseable ?? true,
		recycle: share?.recycle.enabled ?? false,
		days: share?.recycle.days ?? 30,
		timeMachine: share?.timeMachine.enabled ?? false,
		sizeGB: share?.timeMachine.sizeGB ?? 0,
		previousVersions: share?.previousVersions ?? false,
		guard: share?.guard ?? false,
		quotaGB: share?.quota ? Math.round(share.quota / 1e7) / 100 : 0,
	});
	let working = $state(false);
	let error = $state('');
	let tab = $state<'general' | 'access' | 'options'>('general');
	const tabs = ['general', 'access', 'options'] as const;
	const initial = JSON.stringify(untrack(() => $state.snapshot(draft)));
	const dirty = $derived(JSON.stringify(draft) !== initial);
	let confirmClose = $state(false),
		pendingURL = '',
		allowNavigation = false;
	const closeNow = () => {
		allowNavigation = true;
		onclose();
		if (pendingURL) void goto(pendingURL);
	};
	function requestClose() {
		if (working) return;
		if (dirty) confirmClose = true;
		else closeNow();
	}
	beforeNavigate((navigation) => {
		if (!dirty || allowNavigation) return;
		navigation.cancel();
		if (navigation.willUnload) return;
		pendingURL = navigation.to?.url.href ?? '';
		confirmClose = true;
	});

	const nameError = $derived(
		draft.name !== '' && !validName(draft.name) ? errorText('invalid_share_name') : '',
	);
	const btrfs = $derived(
		storage.status?.volumes.find((v) => v.name === draft.volume)?.filesystem === 'btrfs',
	);
	const quotaState = $derived(
		storage.status?.volumes.find((v) => v.name === draft.volume)?.shareQuotas ?? 'off',
	);
	const quotaTooSmall = $derived(
		Number(draft.quotaGB) > 0 && Number(draft.quotaGB) * 1e9 < 16 * 2 ** 20,
	);
	const nobody = $derived(draft.smb && !draft.guest && Object.keys(draft.access).length === 0);

	// NFS clients as exportfs takes them: an IPv4/IPv6 address or network, or a
	// host name (wildcards allowed). The backend checks again.
	const lines = (text: string) =>
		text
			.split('\n')
			.map((line) => line.trim())
			.filter(Boolean);
	function validClient(client: string): boolean {
		if (!/^[A-Za-z0-9*][A-Za-z0-9.:/*_-]{0,99}$/.test(client)) return false;
		const ipv4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?:\/(\d{1,2}))?$/.exec(client);
		if (ipv4)
			return (
				ipv4.slice(1, 5).every((part) => Number(part) <= 255) &&
				(ipv4[5] === undefined || Number(ipv4[5]) <= 32)
			);
		if (/^[\d./]+$/.test(client)) return false;
		if (client.includes(':')) {
			const [address, prefix, ...rest] = client.split('/');
			if (
				rest.length ||
				(prefix !== undefined && !(/^\d{1,3}$/.test(prefix) && Number(prefix) <= 128))
			)
				return false;
			const halves = address.split('::');
			if (halves.length > 2) return false;
			const groups = halves.flatMap((half) => (half ? half.split(':') : []));
			return (
				groups.every((group) => /^[0-9A-Fa-f]{1,4}$/.test(group)) &&
				(halves.length === 2 ? groups.length < 8 : groups.length === 8)
			);
		}
		if (client.includes('/')) return false;
		return client
			.split('.')
			.every((label) => /^[A-Za-z0-9*_](?:[A-Za-z0-9*_-]{0,61}[A-Za-z0-9*_])?$/.test(label));
	}
	const badClients = $derived(
		draft.nfs === 'off' ? [] : lines(draft.clients).filter((client) => !validClient(client)),
	);
	const noClients = $derived(draft.nfs !== 'off' && lines(draft.clients).length === 0);

	// The tab that holds the field a refusal from the NAS is about.
	const ERROR_TABS: Record<string, typeof tab> = {
		invalid_share_name: 'general',
		share_exists: 'general',
		unknown_volume: 'general',
		volume_change: 'general',
		invalid_nfs_client: 'general',
		unknown_user: 'access',
		invalid_access: 'access',
	};

	function setAccess(user: string, level: '' | AccessLevel) {
		if (level === '') delete draft.access[user];
		else draft.access[user] = level;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!validName(draft.name))
			return void ((error = errorText('invalid_share_name')), (tab = 'general'));
		if (badClients.length || noClients)
			return void ((error = errorText('invalid_nfs_client')), (tab = 'general'));
		working = true;
		error = '';
		const body = {
			name: draft.name,
			volume: draft.volume,
			smb: draft.smb,
			nfs: { enabled: draft.nfs !== 'off', mode: draft.nfs, clients: lines(draft.clients) },
			access: $state.snapshot(draft.access),
			guest: draft.smb && draft.guest,
			browseable: draft.browseable,
			recycle: { enabled: draft.smb && draft.recycle, days: Number(draft.days) || 30 },
			timeMachine: { enabled: draft.smb && draft.timeMachine, sizeGB: Number(draft.sizeGB) || 0 },
			previousVersions: draft.smb && draft.previousVersions,
			guard: draft.smb && draft.guard,
			quota: Math.round((Number(draft.quotaGB) || 0) * 1e9),
		};
		try {
			if (share) await api.put(`/shares/${encodeURIComponent(share.name)}`, body);
			else await api.post('/shares', body);
			allowNavigation = true;
			ondone();
			if (pendingURL) void goto(pendingURL);
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = errorText(code);
			// Out-of-range values are the quota (general) or the SMB options.
			const where =
				ERROR_TABS[code] ??
				(code === 'invalid_option'
					? Number(draft.quotaGB) > 0 && !draft.smb
						? 'general'
						: 'options'
					: undefined);
			if (where && (where === 'general' || draft.smb)) tab = where;
			working = false;
		}
	}
</script>

<Dialog
	title={share ? t('share.editTitle', { name: share.name }) : t('share.createTitle')}
	wide
	locked={working}
	onrequestclose={requestClose}
	{onclose}
>
	<div class="tabs" role="tablist" aria-label={t('shares.details')}>
		{#each tabs as id}<button
				type="button"
				role="tab"
				aria-selected={tab === id}
				onclick={() => (tab = id)}>{t(`shares.tab.${id}`)}</button
			>{/each}
	</div>
	{#if confirmClose}<div class="notice-warn" role="alert">
			<p>{t('shares.unsaved')}</p>
			<div class="close-options dialog-actions">
				<button
					class="btn"
					type="button"
					onclick={() => {
						confirmClose = false;
						pendingURL = '';
					}}>{t('shares.keepEditing')}</button
				><button class="btn btn-warn" type="button" onclick={closeNow}>{t('shares.discard')}</button
				><button
					class="btn btn-primary"
					type="button"
					onclick={() => {
						confirmClose = false;
						(document.getElementById(formID) as HTMLFormElement | null)?.requestSubmit();
					}}>{t('common.save')}</button
				>
			</div>
		</div>{/if}
	<form id={formID} onsubmit={submit}>
		<div class="general" hidden={tab !== 'general'}>
			<div class="two">
				<label class="field-row">
					<span class="label">{t('share.name')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="field mono"
						type="text"
						bind:value={draft.name}
						maxlength="32"
						disabled={!!share}
						spellcheck="false"
						autofocus={!share}
					/>
					<small class:problem={nameError}>{nameError || t('share.nameHint')}</small>
				</label>
				<label class="field-row">
					<span class="label">{t('share.volume')}</span>
					<select class="field" bind:value={draft.volume} disabled={!!share}>
						{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
					</select>
					<small>{t('share.location', { path: `/mnt/${draft.volume}/${draft.name || '…'}` })}</small
					>
				</label>
			</div>
			{#if quotaState !== 'unsupported' || draft.quotaGB}
				<label class="field-row">
					<span class="label">{t('share.quota')}</span>
					<input class="field number" type="number" min="0" step="0.1" bind:value={draft.quotaGB} />
					<small class:problem={quotaTooSmall}
						>{quotaTooSmall ? t('share.quotaTooSmall') : t(`share.quotaHint.${quotaState}`)}</small
					>
				</label>
			{:else}<p class="muted hint">{t('share.quotaHint.unsupported')}</p>{/if}

			<fieldset>
				<legend>{t('share.protocols')}</legend>
				<label class="check"
					><input type="checkbox" bind:checked={draft.smb} /><span>{t('share.smb')}</span></label
				>
				<label class="field-row"
					><span class="label">{t('share.nfs')}</span><select class="field" bind:value={draft.nfs}
						><option value="off">{t('share.nfsOff')}</option><option value="ro"
							>{t('share.ro')}</option
						><option value="rw">{t('share.rw')}</option></select
					></label
				>
				{#if draft.nfs !== 'off'}
					<label class="field-row indent">
						<span class="label">{t('share.nfsClients')}</span>
						<textarea
							class="field mono"
							rows="3"
							bind:value={draft.clients}
							placeholder="192.168.1.0/24"
							spellcheck="false"
							aria-invalid={badClients.length > 0 || noClients}
							aria-describedby="{formID}-clients"></textarea>
						<small id="{formID}-clients" class:problem={badClients.length > 0 || noClients}
							>{badClients.length
								? t('share.nfsClientsInvalid', { clients: badClients.join(', ') })
								: noClients
									? t('share.nfsClientsNone')
									: t('share.nfsClientsHint')}</small
						>
					</label>
					<p class="notice-warn">{t('share.nfsWarning')}</p>
					{#if draft.nfs === 'ro'}<p class="muted">{t('share.nfsReadOnlyHint')}</p>{/if}
				{/if}
			</fieldset>
		</div>
		{#if draft.smb}
			<fieldset hidden={tab !== 'access'}>
				<legend>{t('share.accessTitle')}</legend>
				{#each accounts as account (account.name)}
					<div class="access">
						<span class="mono">{account.name}</span>
						<span class="muted role">{t(`role.${account.role}`)}</span>
						<div
							class="segments"
							role="radiogroup"
							aria-label={account.name}
							tabindex="-1"
							onkeydown={radioKeys}
						>
							{#each LEVELS as level (level)}
								<button
									type="button"
									role="radio"
									aria-checked={(draft.access[account.name] ?? '') === level}
									tabindex={(draft.access[account.name] ?? '') === level ? 0 : -1}
									onclick={() => setAccess(account.name, level)}
								>
									{t(level === '' ? 'share.none' : `share.${level}`)}
								</button>
							{/each}
						</div>
					</div>
				{/each}
				<label class="check"
					><input type="checkbox" bind:checked={draft.guest} /><span>{t('share.guest')}</span
					></label
				>
				{#if draft.guest}<p class="notice-warn" role="alert">{t('share.guestWarning')}</p>{/if}
				{#if nobody}<p class="notice-warn" role="alert">{t('share.nobodyWarning')}</p>{/if}
			</fieldset>

			<fieldset hidden={tab !== 'options'}>
				<label class="check"
					><input type="checkbox" bind:checked={draft.browseable} /><span
						>{t('share.browseable')}</span
					></label
				>
				<label class="check"
					><input type="checkbox" bind:checked={draft.recycle} /><span>{t('share.recycle')}</span
					></label
				>
				{#if draft.recycle}
					<label class="inline indent"
						><span>{t('share.recycleDays')}</span><input
							class="field number"
							type="number"
							min="1"
							max="3650"
							bind:value={draft.days}
						/></label
					>
				{/if}
				<label class="check"
					><input type="checkbox" bind:checked={draft.timeMachine} /><span
						>{t('share.timeMachine')}</span
					></label
				>
				{#if draft.timeMachine}
					<label class="inline indent"
						><span>{t('share.timeMachineSize')}</span><input
							class="field number"
							type="number"
							min="0"
							bind:value={draft.sizeGB}
						/></label
					>
				{/if}
				<label class="check"
					><input type="checkbox" bind:checked={draft.previousVersions} disabled={!btrfs} /><span
						>{t('share.previousVersions')}</span
					></label
				>
				<p class="muted indent small">
					{t(btrfs ? 'share.previousVersionsHint' : 'share.previousVersionsBtrfs')}
				</p>
				<label class="check"
					><input type="checkbox" bind:checked={draft.guard} /><span>{t('share.guard')}</span
					></label
				>
				<p class="muted indent small">{t('share.guardHint')}</p>
			</fieldset>
		{:else if tab !== 'general'}<p class="muted">{t('shares.smbOptions')}</p>
		{/if}

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={requestClose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button
				type="submit"
				class="btn btn-primary"
				disabled={working || !draft.volume || quotaTooSmall}
				>{t(share ? 'share.save' : 'share.create')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	[hidden] {
		display: none !important;
	}
	.general {
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	.tabs {
		display: flex;
		gap: 20px;
		border-bottom: 1px solid var(--panel-border);
	}
	.tabs button {
		font: inherit;
		background: none;
		color: var(--muted);
		border: 0;
		border-bottom: 2px solid transparent;
		padding: 8px 0;
		cursor: pointer;
	}
	.tabs button[aria-selected='true'] {
		color: var(--accent);
		border-color: var(--accent);
	}
	.close-options {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 12px;
	}

	form {
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	.two {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: 16px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.label,
	legend {
		font-size: var(--text-md);
		font-weight: 500;
	}
	legend {
		padding: 0;
		margin-bottom: 4px;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	small.problem {
		color: var(--warn-text);
	}
	.check {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		font-size: var(--text-md);
	}
	.check input {
		width: 18px;
		height: 18px;
		flex: none;
		margin: 2px 0 0;
	}
	.indent {
		margin-left: 28px;
	}
	.inline {
		display: flex;
		align-items: center;
		gap: 12px;
		font-size: var(--text-md);
	}
	.number {
		width: 7rem;
		height: 2.25rem;
	}
	textarea {
		height: auto;
		padding: 0.5rem 0.75rem;
		resize: vertical;
	}
	.access {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
		padding: 8px 12px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-md);
	}
	.role {
		flex: 1 1 auto;
		font-size: var(--text-base);
	}
	.segments {
		display: flex;
		gap: 4px;
		padding: 3px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 30px;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 500;
	}
</style>
