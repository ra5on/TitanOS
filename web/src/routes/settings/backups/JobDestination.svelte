<script lang="ts">
	// Where a backup goes: the place, its format (readable copy or encrypted
	// versions) and the fields of that kind of destination.
	import ClusterPair from '#lib/components/ClusterPair.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		decodeCode,
		kindOf,
		PLACES,
		placeOf,
		plainCopy,
		twoFormats,
		type Job,
		type Kind,
		type Overview,
		type UsbDrive,
	} from './data-backups.ts';

	let {
		draft = $bindable(),
		pairCode = $bindable(),
		isNew,
		overview,
		volumes,
		usbDrives,
		usbError,
		loadUSB,
		publicKey,
		showKey,
		secretSet,
		secretKept,
	}: {
		draft: Job;
		/** A pairing code of another TitanOS: titanos-backup: and base64url JSON. */
		pairCode: string;
		isNew: boolean;
		overview: Overview;
		volumes: string[];
		usbDrives: UsbDrive[];
		usbError: string;
		loadUSB: () => void;
		publicKey: string;
		showKey: () => void;
		secretSet: Record<string, boolean>;
		/** Whether the stored secret stays: only for the same server and account. */
		secretKept: { secretKey: boolean; password: boolean };
	} = $props();

	const pairing = $derived(decodeCode(pairCode));

	function setKind(kind: Kind) {
		if (!draft) return;
		draft.destination.type = kind;
		if (plainCopy(kind)) {
			draft.applications = [];
			draft.allowStop = false;
			draft.extras = [];
			draft.copy ??= {};
		}
		if (kind === 'sftp') showKey();
		if (kind === 'titanos') pairCode = '';
	}
</script>

<fieldset class="divided">
	<legend>{t('backup.destination')}</legend>
	<div class="segments" role="radiogroup" aria-label={t('backup.destination')}>
		{#each PLACES as place (place)}
			{@const kind = kindOf(place, plainCopy(draft.destination.type))}
			<button
				type="button"
				role="radio"
				aria-checked={placeOf(draft.destination.type) === place}
				disabled={!isNew && plainCopy(kind) !== plainCopy(draft.destination.type)}
				onclick={() => setKind(kind)}>{t(`backup.place.${place}` as MessageKey)}</button
			>
		{/each}
	</div>
	{#if twoFormats(placeOf(draft.destination.type))}
		{@const place = placeOf(draft.destination.type)}
		<p class="muted">{t(`backup.placeHint.${place}` as MessageKey)}</p>
		<div class="formats" role="radiogroup" aria-label={t('backup.format')}>
			{#each [true, false] as readable (readable)}
				{@const on = plainCopy(draft.destination.type) === readable}
				<div class="format" class:on>
					<label class="fhead"
						><input
							type="radio"
							name="format"
							checked={on}
							disabled={!isNew && !on}
							onchange={() => setKind(kindOf(place, readable))}
						/><strong>{t(readable ? 'backup.format.readable' : 'backup.format.encrypted')}</strong
						></label
					>
					<span class="muted small"
						>{t(readable ? 'backup.format.readableHint' : 'backup.format.encryptedHint')}</span
					>
					{#if readable && on}
						<label class="dated"
							><input
								type="checkbox"
								checked={!!draft.copy?.dated}
								onchange={(e) => (draft!.copy = { ...draft!.copy, dated: e.currentTarget.checked })}
							/><span class="small">{t('backup.copy.dated')}</span></label
						>
					{/if}
				</div>
			{/each}
		</div>
		{#if !isNew}<p class="muted small">{t('backup.format.fixed')}</p>{/if}
	{:else}
		<p class="muted">{t(`backup.kindHint.${draft.destination.type}` as MessageKey)}</p>
	{/if}
	{#if draft.destination.type === 'smb-copy' || draft.destination.type === 'smb'}
		<label class="block"
			><span>{t('backup.smb.target')}</span><select
				class="field"
				bind:value={draft.destination.targetId}
				required
				><option value="" disabled>–</option
				>{#each overview?.smbTargets ?? [] as target (target.id)}<option value={target.id}
						>{target.id} · {target.host}/{target.share}</option
					>{/each}</select
			></label
		>
		<p class="muted">{t('backup.smb.first')}</p>
		<label class="block"
			><span>{t('backup.smb.jobFolder')}</span><input
				class="field"
				bind:value={draft.destination.folder}
				placeholder="backups/{draft.id}"
			/></label
		>
	{:else if draft.destination.type === 'usb-copy' || draft.destination.type === 'usb'}
		{#if usbError}<p class="notice-warn" role="alert">
				{usbError}
				<button type="button" class="btn small" onclick={loadUSB}>{t('ui.retry')}</button>
			</p>{/if}
		<label class="block"
			><span>{t('backup.usbDevice')}</span><select
				class="field"
				bind:value={draft.destination.deviceId}
				required
			>
				<option value="" disabled>–</option>
				{#if draft.destination.deviceId && !usbDrives.some((d) => d.id === draft?.destination.deviceId)}<option
						value={draft.destination.deviceId}
						>{draft.destination.deviceName || draft.destination.deviceId} · {t(
							'backup.usbMissing',
						)}</option
					>{/if}
				{#each usbDrives.filter((d) => d.supported) as drive (drive.id)}<option value={drive.id}
						>{drive.name} · {drive.id}</option
					>{/each}
			</select></label
		>
		<label class="block"
			><span>{t('backup.folder')}</span><input
				class="field mono"
				bind:value={draft.destination.folder}
				placeholder={`backups/${draft.id || 'name'}`}
			/></label
		>
	{:else if draft.destination.type === 'local' || draft.destination.type === 'local-copy'}
		<div class="row">
			<label class="narrow"
				><span>{t('backup.volume')}</span>
				<select class="field" bind:value={draft.destination.volume} required>
					<option value="" disabled>–</option>
					{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
				</select>
			</label>
			<label class="narrow"
				><span>{t('backup.folder')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.folder}
					placeholder={`backups/${draft.id || 'name'}`}
					spellcheck="false"
				/></label
			>
		</div>
		{#if volumes.length === 0}<p class="notice-warn">{t('backup.noVolume')}</p>{/if}
	{:else if draft.destination.type === 's3'}
		<div class="row">
			<label class="wide"
				><span>{t('backup.endpoint')}</span><input
					class="field mono"
					type="url"
					bind:value={draft.destination.endpoint}
					placeholder="https://s3.eu-central-1.amazonaws.com"
					spellcheck="false"
					required
				/></label
			>
			<label class="narrow"
				><span>{t('backup.bucket')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.bucket}
					spellcheck="false"
					autocapitalize="none"
					required
				/></label
			>
			<label class="narrow"
				><span>{t('backup.prefix')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.prefix}
					spellcheck="false"
				/></label
			>
		</div>
		<div class="row">
			<label class="narrow"
				><span>{t('backup.accessKey')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.accessKey}
					autocomplete="off"
					spellcheck="false"
					required
				/></label
			>
			<label class="narrow"
				><span>{t('backup.secretKey')}</span><input
					class="field mono"
					type="password"
					bind:value={draft.destination.secretKey}
					autocomplete="new-password"
					placeholder={secretKept.secretKey ? t('notify.secretStored') : ''}
					required={!secretKept.secretKey}
				/></label
			>
		</div>
		{#if secretSet.secretKey && !secretKept.secretKey}<p class="notice-warn">
				{t('backup.secretAgain')}
			</p>{/if}
	{:else if draft.destination.type === 'sftp'}
		<div class="row">
			<label class="narrow"
				><span>{t('backup.host')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.host}
					spellcheck="false"
					autocapitalize="none"
					required
				/></label
			>
			<label class="tiny"
				><span>{t('notify.port')}</span><input
					class="field mono"
					type="number"
					min="1"
					max="65535"
					bind:value={draft.destination.port}
					required
				/></label
			>
			<label class="narrow"
				><span>{t('notify.username')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.user}
					spellcheck="false"
					autocapitalize="none"
					required
				/></label
			>
			<label class="narrow"
				><span>{t('backup.remotePath')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.path}
					placeholder="backups/nas"
					spellcheck="false"
					required
				/></label
			>
		</div>
		{#if publicKey}
			<label
				><span>{t('backup.publicKey')}</span><textarea
					class="field mono key"
					readonly
					rows="2"
					onfocus={(event) => event.currentTarget.select()}>{publicKey}</textarea
				></label
			>
			<p class="muted">{t('backup.publicKeyHint')}</p>
		{/if}
	{:else if draft.destination.type === 'titanos'}
		<ClusterPair onpair={(code) => (pairCode = code)} />
		<label class="block"
			><span>{t('backup.pairCode')}</span>
			<textarea
				class="field mono"
				rows="3"
				bind:value={pairCode}
				spellcheck="false"
				autocomplete="off"
				placeholder="titanos-backup:…"
				required></textarea>
		</label>
		{#if pairing}<p class="notice-ok">
				{t('backup.pairCodeOk', { url: pairing.url })}
			</p>{:else if pairCode.trim()}<p class="notice-warn">
				{t('backup.pairCodeInvalid')}
			</p>{/if}
		<p class="muted">{t('backup.pairCodeHint')}</p>
	{:else}
		<div class="row">
			<label class="wide"
				><span>{t('backup.restUrl')}</span><input
					class="field mono"
					type="url"
					bind:value={draft.destination.url}
					placeholder="https://backup.example.org:8000/nas"
					spellcheck="false"
					required
				/></label
			>
			<label class="narrow"
				><span>{t('notify.username')}</span><input
					class="field mono"
					type="text"
					bind:value={draft.destination.username}
					autocomplete="off"
					spellcheck="false"
				/></label
			>
			<label class="narrow"
				><span>{t('notify.password')}</span><input
					class="field mono"
					type="password"
					bind:value={draft.destination.password}
					autocomplete="new-password"
					placeholder={secretKept.password ? t('notify.secretStored') : ''}
				/></label
			>
		</div>
		{#if secretSet.password && !secretKept.password}<p class="notice-warn">
				{t('backup.secretAgain')}
			</p>{/if}
	{/if}
</fieldset>

<style>
	p {
		margin: 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3) 20px;
	}
	fieldset {
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.narrow {
		flex: 0 1 260px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 140px;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 560px;
	}
	.formats {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2-5);
	}
	.format {
		display: grid;
		gap: var(--space-1);
		align-content: start;
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: var(--space-3) 14px;
	}
	.format.on {
		border-color: var(--accent);
	}
	.format .fhead,
	.format .dated {
		display: flex;
		flex-direction: row;
		gap: var(--space-2);
		align-items: center;
		justify-content: flex-start;
		text-align: left;
		font-size: inherit;
	}
	.format .dated {
		align-items: flex-start;
		margin: var(--space-1-5) 0 0 var(--space-5);
	}
	.format > .muted {
		margin-left: var(--space-5);
	}
	.small {
		font-size: 0.8125rem;
	}
	@media (max-width: 760px) {
		.formats {
			grid-template-columns: 1fr;
		}
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		align-self: flex-start;
		gap: var(--space-1);
		padding: 3px;
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
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.key {
		height: auto;
		padding: var(--space-2-5) var(--space-3);
		font-size: 0.75rem;
		resize: none;
	}
	.block {
		display: grid;
		gap: 0.3rem;
		margin-top: 0.6rem;
	}
</style>
