<script lang="ts">
 import { untrack } from 'svelte';
 import { beforeNavigate, goto } from '$app/navigation';
	import { api, ApiError, type AccessLevel, type Account, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';

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

	// svelte-ignore state_referenced_locally
	let draft = $state({
		name: share?.name ?? '',
		volume: share?.volume ?? volumes[0] ?? '',
		smb: share?.smb ?? true,
		nfs: share?.nfs.enabled ?? false,
		clients: (share?.nfs.clients ?? []).join('\n'),
		// Whoever creates a share can use it; Samba does not offer a share no one may open.
		access: { ...(share?.access ?? (session.data?.user ? { [session.data.user.name]: 'rw' } : {})) } as Record<string, AccessLevel>,
		guest: share?.guest ?? false,
		browseable: share?.browseable ?? true,
		recycle: share?.recycle.enabled ?? false,
		days: share?.recycle.days ?? 30,
		timeMachine: share?.timeMachine.enabled ?? false,
		sizeGB: share?.timeMachine.sizeGB ?? 0
	});
	let working = $state(false);
	let error = $state('');
 let tab = $state<'general'|'access'|'options'>('general');
 const tabs = ['general','access','options'] as const;
 const initial = JSON.stringify(untrack(() => $state.snapshot(draft)));
 const dirty = $derived(JSON.stringify(draft) !== initial);
 let confirmClose = $state(false), pendingURL = '', allowNavigation = false;
 const closeNow = () => { allowNavigation=true; onclose(); if(pendingURL) void goto(pendingURL); };
 function requestClose() { if(working)return; if(dirty)confirmClose=true; else closeNow(); }
 beforeNavigate(navigation => {
  if(!dirty||allowNavigation)return;
  navigation.cancel();
  if(navigation.willUnload)return;
  pendingURL=navigation.to?.url.href ?? ''; confirmClose=true;
 });

	const nameError = $derived(draft.name !== '' && !validName(draft.name) ? errorText('invalid_share_name') : '');
	const nobody = $derived(draft.smb && !draft.guest && Object.keys(draft.access).length === 0);

	function setAccess(user: string, level: '' | AccessLevel) {
		if (level === '') delete draft.access[user];
		else draft.access[user] = level;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!validName(draft.name)) return void (error = errorText('invalid_share_name'));
		working = true;
		error = '';
		const body = {
			name: draft.name,
			volume: draft.volume,
			smb: draft.smb,
			nfs: { enabled: draft.nfs, clients: draft.clients.split('\n').map((line) => line.trim()).filter(Boolean) },
			access: $state.snapshot(draft.access),
			guest: draft.smb && draft.guest,
			browseable: draft.browseable,
			recycle: { enabled: draft.smb && draft.recycle, days: Number(draft.days) || 30 },
			timeMachine: { enabled: draft.smb && draft.timeMachine, sizeGB: Number(draft.sizeGB) || 0 }
		};
		try {
			if (share) await api.put(`/shares/${encodeURIComponent(share.name)}`, body);
			else await api.post('/shares', body);
   allowNavigation=true;
			ondone();
   if(pendingURL) void goto(pendingURL);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			working = false;
		}
	}
</script>

<Dialog title={share ? t('share.editTitle', { name: share.name }) : t('share.createTitle')} wide locked={working} onrequestclose={requestClose} {onclose}>
	<div class="tabs" role="tablist" aria-label={t('shares.details')}>
 {#each tabs as id}<button type="button" role="tab" aria-selected={tab===id} onclick={()=>tab=id}>{t(`shares.tab.${id}`)}</button>{/each}
 </div>
 {#if confirmClose}<div class="notice-warn" role="alert"><p>{t('shares.unsaved')}</p><div class="close-options dialog-actions"><button class="btn" type="button" onclick={()=>{confirmClose=false;pendingURL='';}}>{t('shares.keepEditing')}</button><button class="btn btn-warn" type="button" onclick={closeNow}>{t('shares.discard')}</button><button class="btn btn-primary" type="button" onclick={()=>{confirmClose=false;(document.getElementById(formID) as HTMLFormElement | null)?.requestSubmit();}}>{t('common.save')}</button></div></div>{/if}
 <form id={formID} onsubmit={submit}>
		<div class="general" hidden={tab!=='general'}>
		<div class="two">
			<label class="field-row">
				<span class="label">{t('share.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field mono" type="text" bind:value={draft.name} maxlength="32" disabled={!!share} spellcheck="false" autofocus={!share} />
				<small class:problem={nameError}>{nameError || t('share.nameHint')}</small>
			</label>
			<label class="field-row">
				<span class="label">{t('share.volume')}</span>
				<select class="field" bind:value={draft.volume} disabled={!!share}>
					{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
				</select>
				<small>{t('share.location', { path: `/mnt/${draft.volume}/${draft.name || '…'}` })}</small>
			</label>
		</div>

		<fieldset>
			<legend>{t('share.protocols')}</legend>
			<label class="check"><input type="checkbox" bind:checked={draft.smb} /><span>{t('share.smb')}</span></label>
			<label class="check"><input type="checkbox" bind:checked={draft.nfs} /><span>{t('share.nfs')}</span></label>
			{#if draft.nfs}
				<label class="field-row indent">
					<span class="label">{t('share.nfsClients')}</span>
					<textarea class="field mono" rows="3" bind:value={draft.clients} placeholder="192.168.1.0/24" spellcheck="false"></textarea>
				</label>
				<p class="notice-warn">{t('share.nfsWarning')}</p>
			{/if}
		</fieldset>

 </div>
		{#if draft.smb}
			<fieldset hidden={tab!=='access'}>
				<legend>{t('share.accessTitle')}</legend>
				{#each accounts as account (account.name)}
					<div class="access">
						<span class="mono">{account.name}</span>
						<span class="muted role">{t(`role.${account.role}`)}</span>
						<div class="segments" role="radiogroup" aria-label={account.name}>
							{#each LEVELS as level (level)}
								<button type="button" role="radio" aria-checked={(draft.access[account.name] ?? '') === level} onclick={() => setAccess(account.name, level)}>
									{t(level === '' ? 'share.none' : `share.${level}`)}
								</button>
							{/each}
						</div>
					</div>
				{/each}
				<label class="check"><input type="checkbox" bind:checked={draft.guest} /><span>{t('share.guest')}</span></label>
				{#if draft.guest}<p class="notice-warn" role="alert">{t('share.guestWarning')}</p>{/if}
				{#if nobody}<p class="notice-warn" role="alert">{t('share.nobodyWarning')}</p>{/if}
			</fieldset>

			<fieldset hidden={tab!=='options'}>
				<label class="check"><input type="checkbox" bind:checked={draft.browseable} /><span>{t('share.browseable')}</span></label>
				<label class="check"><input type="checkbox" bind:checked={draft.recycle} /><span>{t('share.recycle')}</span></label>
				{#if draft.recycle}
					<label class="inline indent"><span>{t('share.recycleDays')}</span><input class="field number" type="number" min="1" max="3650" bind:value={draft.days} /></label>
				{/if}
				<label class="check"><input type="checkbox" bind:checked={draft.timeMachine} /><span>{t('share.timeMachine')}</span></label>
				{#if draft.timeMachine}
					<label class="inline indent"><span>{t('share.timeMachineSize')}</span><input class="field number" type="number" min="0" bind:value={draft.sizeGB} /></label>
				{/if}
			</fieldset>
		{:else if tab!=='general'}<p class="muted">{t('shares.smbOptions')}</p>
		{/if}

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={requestClose} disabled={working}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={working || !draft.volume}>{t(share ? 'share.save' : 'share.create')}</button>
		</div>
	</form>
</Dialog>

<style>
 [hidden]{display:none!important}.general{display:flex;flex-direction:column;gap:18px}.tabs{display:flex;gap:20px;border-bottom:1px solid var(--panel-border)}.tabs button{font:inherit;background:none;color:var(--muted);border:0;border-bottom:2px solid transparent;padding:8px 0;cursor:pointer}.tabs button[aria-selected=true]{color:var(--accent);border-color:var(--accent)}.close-options{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px}

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
		font-size: 0.875rem;
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
		font-size: 0.75rem;
		color: var(--muted);
	}
	small.problem {
		color: var(--warn-text);
	}
	.check {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		font-size: 0.875rem;
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
		font-size: 0.875rem;
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
		font-size: 0.875rem;
	}
	.role {
		flex: 1 1 auto;
		font-size: 0.8125rem;
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
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 500;
	}
</style>
