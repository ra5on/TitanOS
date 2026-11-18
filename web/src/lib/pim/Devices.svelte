<script lang="ts">
	// "1. Gerätepasswort" of the iPhone setup (TECHNIK 3.2, 3.5; draft 108,
	// pictures 20/21/60): the devices with their last sync and "Entziehen …",
	// and a new password, made with the account password (and a code with
	// two-factor login) and shown once – only in this component's state,
	// never in the browser's storage; gone after a reload.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { formatDate, formatTime } from '#lib/format.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { lastSync, passwordGroups, type CreatedDevice, type Device } from '#lib/pim/devices.ts';

	const phone = $derived(phoneView());

	let devices = $state<Device[] | null>(null);
	let max = $state(20);
	let error = $state('');
	let name = $state('');
	// The proof the NAS asks for (dialog), and the password shown once.
	let asking = $state(false);
	let password = $state('');
	let code = $state('');
	let askError = $state('');
	let busy = $state(false);
	let shown = $state<CreatedDevice | null>(null);
	let copied = $state(false);
	let revoking = $state<Device | null>(null);
	let revokeError = $state('');

	async function load() {
		try {
			const r = await api.get<{ devices: Device[]; max: number }>('/account/devices');
			devices = r.devices ?? [];
			max = r.max || 20;
			error = '';
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	$effect(() => {
		load();
	});

	const full = $derived(!!devices && devices.length >= max);

	function dayMonth(d: Date): string {
		return new Intl.DateTimeFormat(i18n.locale, { day: 'numeric', month: 'numeric' }).format(d);
	}

	function syncText(d: Device): string {
		const s = lastSync(d, new Date());
		switch (s.when) {
			case 'never':
				return t('iphone.sync.never');
			case 'beforeRestart':
				return t('iphone.sync.beforeRestart', { date: dayMonth(s.at) });
			default: {
				const time = formatTime(s.at);
				const text =
					s.when === 'today'
						? t('iphone.sync.today', { time })
						: s.when === 'yesterday'
							? t('iphone.sync.yesterday', { time })
							: t('iphone.sync.on', { date: dayMonth(s.at), time });
				if (!s.kind) return text;
				return `${text} (${t(s.kind === 'caldav' ? 'iphone.kind.caldav' : 'iphone.kind.carddav')})`;
			}
		}
	}

	function deviceDesc(d: Device): string {
		return `${t('iphone.devices.created', { date: formatDate(new Date(d.created)) })} · ${syncText(d)}`;
	}

	function ask(event?: SubmitEvent) {
		event?.preventDefault();
		if (!name.trim() || full) return;
		password = '';
		code = '';
		askError = '';
		asking = true;
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		askError = '';
		try {
			shown = await api.post<CreatedDevice>('/account/devices', {
				name: name.trim(),
				password,
				code: code.trim(),
			});
			copied = false;
			asking = false;
			password = '';
			code = '';
			name = '';
			await load();
		} catch (err) {
			askError = apiErrorText(err);
			// A used or wrong code cannot be sent again.
			if (err instanceof ApiError && err.code === 'invalid_code') code = '';
		} finally {
			busy = false;
		}
	}

	async function copy() {
		if (!shown) return;
		try {
			await navigator.clipboard.writeText(shown.password);
			copied = true;
		} catch {
			copied = false;
		}
	}

	async function revoke() {
		if (!revoking) return;
		busy = true;
		revokeError = '';
		try {
			await api.delete(`/account/devices/${encodeURIComponent(revoking.id)}`);
			if (shown?.id === revoking.id) shown = null;
			revoking = null;
			await load();
		} catch (err) {
			revokeError = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<SettingGroup title={t('iphone.devices.title')} desc={t('iphone.devices.desc')} id="devices">
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#each devices ?? [] as d (d.id)}
		<SettingRow icon="device" title={d.name} desc={deviceDesc(d)}>
			{#snippet control()}<button type="button" class="btn" onclick={() => (revoking = d)}
					>{t('iphone.devices.revoke')}</button
				>{/snippet}
		</SettingRow>
	{/each}
	{#if shown}
		<div class="srow plain shown" role="status">
			<div class="once">
				<b>{t('iphone.once.title', { name: shown.name })}</b>
				<span class="pw">
					<code class="secret"
						>{#each passwordGroups(shown.password) as g, i (i)}<span class="grp">{g}</span
							>{/each}</code
					>
					<button type="button" class="btn cp" onclick={copy}
						><Icon name={copied ? 'check' : 'copy'} size={15} />{copied
							? t('iphone.copied')
							: t('iphone.copy')}</button
					>
				</span>
				<small class="muted">{t('iphone.once.hint')}</small>
			</div>
		</div>
	{/if}
	<SettingRow
		icon="plus"
		title={t('iphone.devices.new')}
		desc={full ? t('iphone.devices.full', { max }) : t('iphone.devices.newDesc')}
		for="dev-name"
	>
		{#snippet control()}
			<form class="new" class:phone onsubmit={ask}>
				<input
					id="dev-name"
					class="field name"
					bind:value={name}
					maxlength="64"
					autocomplete="off"
					placeholder={t('iphone.devices.namePlaceholder')}
					disabled={full}
				/>
				<button type="submit" class="btn btn-primary" disabled={!name.trim() || full || busy}
					>{t('iphone.devices.create')}</button
				>
			</form>
		{/snippet}
	</SettingRow>
</SettingGroup>

{#if asking}
	<Dialog
		title={t('iphone.devices.createTitle', { name: name.trim() })}
		onclose={() => (asking = false)}
	>
		<form class="ask" onsubmit={create}>
			<ReconfirmFields bind:password bind:code />
			{#if askError}<p class="notice-error" role="alert">{askError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={busy || !reconfirmComplete(password, code)}
					>{t('iphone.devices.createConfirm')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if revoking}
	<Dialog
		title={t('iphone.devices.revokeTitle')}
		onclose={() => {
			revoking = null;
			revokeError = '';
		}}
	>
		<p>{t('iphone.devices.revokeText', { name: revoking.name })}</p>
		{#if revokeError}<p class="notice-error" role="alert">{revokeError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (revoking = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={revoke}
				>{t('iphone.devices.revokeConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.new {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.name {
		width: 14rem;
		max-width: 100%;
	}
	.phone .name {
		width: 100%;
	}
	.cp {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.shown {
		padding: var(--space-3);
		border: 1px solid var(--ok-border);
		background: var(--ok-bg);
	}
	.once {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		min-width: 0;
	}
	.pw {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.secret {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: var(--text-lg);
		letter-spacing: 0.04em;
		padding: 4px 8px;
		border-radius: var(--radius-small);
		background: var(--surface);
		min-width: 0;
	}
	.grp {
		display: inline-block;
		white-space: nowrap;
	}
	.ask {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	small {
		font-size: var(--text-sm);
	}
</style>
