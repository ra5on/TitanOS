<script lang="ts">
	// Overview: plan and usage, the account log page by page, API keys.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { KEY_LIFETIMES, USAGE, usagePercent, usageText, type ApiKey, type LogEntry, type Me, type Status } from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status }: { status: Status } = $props();

	let me = $state<Me | null>(null);
	let meError = $state('');
	let logs = $state<LogEntry[]>([]);
	let nextBefore = $state<number | null>(null);
	let logError = $state('');
	let logBusy = $state(false);
	let keys = $state<ApiKey[]>([]);
	let keyError = $state('');
	let keyBusy = $state(false);
	let creating = $state(false);
	let newName = $state('');
	let lifetime = $state<number>(KEY_LIFETIMES[2]);
	let created = $state<{ name: string; token: string } | null>(null);
	let removing = $state<ApiKey | null>(null);

	async function loadMe() {
		try {
			me = (await api.get<{ me: Me }>('/ipv64/me')).me;
			meError = '';
		} catch (err) {
			meError = failure(err);
		}
	}
	async function loadLogs(more = false) {
		logBusy = true;
		try {
			const page = await api.get<{ entries: LogEntry[]; next_before: number | null }>(`/ipv64/logs?limit=50${more && nextBefore ? `&before=${nextBefore}` : ''}`);
			logs = more ? [...logs, ...page.entries] : page.entries;
			nextBefore = page.next_before;
			logError = '';
		} catch (err) {
			logError = failure(err);
		} finally {
			logBusy = false;
		}
	}
	async function loadKeys() {
		try {
			keys = (await api.get<{ keys: ApiKey[] }>('/ipv64/api-keys')).keys;
			keyError = '';
		} catch (err) {
			keyError = failure(err);
		}
	}
	onMount(() => {
		loadMe();
		loadLogs();
		loadKeys();
	});

	async function createKey() {
		keyBusy = true;
		keyError = '';
		try {
			const answer = await api.post<{ key: ApiKey; token: string }>('/ipv64/api-keys', { name: newName.trim(), expiresIn: lifetime });
			created = { name: answer.key.name, token: answer.token };
			creating = false;
			newName = '';
			await loadKeys();
		} catch (err) {
			keyError = failure(err);
		} finally {
			keyBusy = false;
		}
	}
	async function removeKey() {
		if (!removing) return;
		keyBusy = true;
		try {
			await api.delete(`/ipv64/api-keys/${removing.id}`);
			removing = null;
			await loadKeys();
		} catch (err) {
			keyError = failure(err);
		} finally {
			keyBusy = false;
		}
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
		} catch {
			// Copying needs a secure context; the field can be selected by hand.
		}
	}
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-plan">
		<h2 class="tile-title" id="ipv64-plan">{t('ipv64x.plan')}</h2>
		{#if meError}<p class="notice-error" role="alert">{meError}</p>{/if}
		{#if me}
			<dl>
				<div><dt>{t('ipv64x.planName')}</dt><dd>{me.plan.name}</dd></div>
				<div><dt>{t('ipv64x.validUntil')}</dt><dd>{me.plan.valid_until ? when(me.plan.valid_until) : t('ipv64x.noEnd')}</dd></div>
				<div><dt>{t('ipv64x.account')}</dt><dd>{me.username ?? '–'}</dd></div>
				<div><dt>{t('ipv64x.keyInUse')}</dt><dd>{me.auth.key_name || me.auth.type}</dd></div>
				<div><dt>{t('ipv64x.retention')}</dt><dd>{t('ipv64x.days', { count: me.log_retention_days })}</dd></div>
			</dl>
			<h3>{t('ipv64x.usage')}</h3>
			<dl class="usage">
				{#each USAGE as name (name)}
					{#if me.usage[name]}
						{@const percent = usagePercent(me.usage[name])}
						<div>
							<dt>{t(`ipv64x.usage.${name}` as MessageKey)}</dt>
							<dd>
								{usageText(me.usage[name])}{#if me.usage[name].limited}<span class="chip error">{t('ipv64x.limited')}</span>{/if}
								{#if percent !== null}<div class="meter" role="meter" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}><span style:width="{percent}%"></span></div>{/if}
							</dd>
						</div>
					{/if}
				{/each}
			</dl>
			<p class="muted hint">{t('ipv64x.planHint')}</p>
		{/if}
	</section>

	<section class="tile panel" aria-labelledby="ipv64-keys">
		<h2 class="tile-title" id="ipv64-keys">{t('ipv64x.apiKeys')}</h2>
		<p class="muted hint">{t('ipv64x.apiKeysHint')}</p>
		{#if keyError}<p class="notice-error" role="alert">{keyError}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.hint')}</th><th>{t('ipv64x.expires')}</th><th>{t('ipv64x.uses')}</th><th></th></tr></thead>
				<tbody>
					{#each keys as key (key.id)}
						<tr>
							<td>{key.name}{#each key.usedBy as use (use)}<span class="chip ok">{t(`ipv64x.usedBy.${use}` as MessageKey)}</span>{/each}</td>
							<td class="mono">{key.token_hint}</td>
							<td>{key.expires_at ? when(key.expires_at) : t('ipv64x.never')}{#if key.expired}<span class="chip error">{t('ipv64x.expired')}</span>{/if}</td>
							<td>{key.uses}</td>
							<td class="actions">
								{#if key.usedBy.length}
									<span class="muted">{t('ipv64x.keyUsed')}</span>
								{:else}
									<button type="button" class="btn" disabled={keyBusy} onclick={() => (removing = key)}>{t('ipv64x.delete')}</button>
								{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="row-actions"><button type="button" class="btn" onclick={() => (creating = true)}>{t('ipv64x.keyCreate')}</button></div>
	</section>

	<section class="tile panel" aria-labelledby="ipv64-log">
		<h2 class="tile-title" id="ipv64-log">{t('ipv64x.log')}</h2>
		{#if logError}<p class="notice-error" role="alert">{logError}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.time')}</th><th>{t('ipv64x.category')}</th><th>{t('ipv64x.message')}</th></tr></thead>
				<tbody>
					{#each logs as entry (entry.id)}
						<tr><td>{when(entry.time)}</td><td>{entry.title}</td><td class="wrap">{entry.message}</td></tr>
					{:else}
						<tr><td colspan="3" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if nextBefore}<div class="row-actions"><button type="button" class="btn" disabled={logBusy} onclick={() => loadLogs(true)}>{t('ipv64x.older')}</button></div>{/if}
	</section>
</div>

{#if creating}
	<Dialog title={t('ipv64x.keyCreate')} locked={keyBusy} onclose={() => (creating = false)}>
		<p>{t('ipv64x.keyCreateText')}</p>
		<label class="stack"><span>{t('ipv64x.name')}</span><input class="field" type="text" maxlength="100" bind:value={newName} /></label>
		<label class="stack"
			><span>{t('ipv64x.lifetime')}</span><select class="field" bind:value={lifetime}>
				{#each KEY_LIFETIMES as seconds (seconds)}<option value={seconds}>{seconds ? t('ipv64x.days', { count: seconds / 86400 }) : t('ipv64x.unlimited')}</option>{/each}
			</select></label
		>
		{#if lifetime === 0}<p class="notice-warn">{t('ipv64x.unlimitedWarn')}</p>{/if}
		{#if keyError}<p class="notice-error" role="alert">{keyError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={keyBusy} onclick={() => (creating = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={keyBusy || !newName.trim()} onclick={createKey}>{t('ipv64x.keyCreate')}</button>
		</div>
	</Dialog>
{/if}

{#if created}
	<Dialog title={t('ipv64x.keyCreated')} onclose={() => (created = null)}>
		<p class="notice-warn">{t('ipv64x.keyOnce', { name: created.name })}</p>
		<input class="field mono" type="text" readonly value={created.token} onfocus={(event) => event.currentTarget.select()} aria-label={t('ipv64x.keyCreated')} />
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => created && copy(created.token)}>{t('ipv64x.copy')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (created = null)}>{t('ipv64x.done')}</button>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('ipv64x.keyDeleteTitle')} locked={keyBusy} onclose={() => (removing = null)}>
		<p>{t('ipv64x.keyDeleteText', { name: removing.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={keyBusy} onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={keyBusy} onclick={removeKey}>{t('ipv64x.delete')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.usage {
		grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
	}
	.chip {
		margin-left: 6px;
	}
</style>
