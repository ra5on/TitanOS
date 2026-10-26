<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import type { NASNetwork } from '#lib/network.ts';
	import type { ContainerLAN } from '#lib/compose/lan.ts';
	let networks = $state<ContainerLAN[]>([]),
		parents = $state<NASNetwork[]>([]),
		loading = $state(true),
		busy = $state(false),
		error = $state(''),
		draft = $state<ContainerLAN | null>(null),
		remove = $state<ContainerLAN | null>(null);
	async function load() {
		try {
			const res = await api.get<{ networks: ContainerLAN[]; parents: NASNetwork[] }>(
				'/docker/networks',
			);
			networks = res.networks ?? [];
			parents = res.parents ?? [];
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});
	function add() {
		draft = {
			name: '',
			parent: parents[0]?.id ?? '',
			driver: 'ipvlan',
			subnet: '',
			gateway: '',
			range: '',
			hostAccess: false,
			hostAddress: '',
		};
		error = '';
	}
	async function create(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		busy = true;
		error = '';
		try {
			const n = $state.snapshot(draft);
			if (!n.hostAccess) n.hostAddress = '';
			await api.post('/docker/networks', n);
			draft = null;
			await load();
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
			await load();
		} finally {
			busy = false;
		}
	}
	async function erase() {
		if (!remove) return;
		busy = true;
		error = '';
		try {
			await api.delete(`/docker/networks/${encodeURIComponent(remove.name)}`);
			remove = null;
			await load();
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('lan.title')} parent={{ href: '/docker', label: t('nav.docker') }}
	><p class="muted">{t('lan.intro')}</p>
	{#snippet actions()}<button
			class="btn btn-primary"
			disabled={!parents.length || busy}
			onclick={add}>{t('lan.add')}</button
		>{/snippet}</PageHead
>
{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
{#if !parents.length && !loading}<p class="notice-warn">
		{t('lan.needParent')} <a href="/settings/network">{t('netcfg.title')}</a>
	</p>{/if}
{#if loading}<p role="status">{t('common.loading')}</p>{:else if !networks.length}<EmptyState
		title={t('lan.empty')}
	/>{:else}<section class="tile content">
		{#each networks as n (n.name)}<article>
				<div class="row">
					<div>
						<strong>{n.name}</strong>
						<p class="muted">
							{parents.find((p) => p.id === n.parent)?.name ?? n.parent} · {n.driver}
						</p>
					</div>
					<button class="btn btn-danger" disabled={busy} onclick={() => (remove = n)}
						>{t('netx.remove')}</button
					>
				</div>
				<p class="mono">{n.subnet} · {n.range}</p>
				<p class="muted">
					{t('netx.gateway')}: {n.gateway}{n.hostAccess
						? ` · ${t('lan.hostAddress')}: ${n.hostAddress}`
						: ''}
				</p>
				{#if n.error}<div class="notice-error" role="alert">{n.error}</div>{/if}
			</article>{/each}
	</section>{/if}
<p class="muted">{t('lan.external')}</p>
{#if draft}<Dialog title={t('lan.add')} wide locked={busy} onclose={() => (draft = null)}
		><form onsubmit={create}>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="two">
				<fieldset>
					<legend>{t('netx.connection')}</legend><label
						><span>{t('netx.name')}</span><input
							class="field"
							required
							pattern="[a-z0-9][a-z0-9_-]*"
							maxlength="24"
							bind:value={draft.name}
						/></label
					><label
						><span>{t('lan.parent')}</span><select class="field" bind:value={draft.parent}
							>{#each parents as p}<option value={p.id}
									>{p.name}{p.vlan ? ` · VLAN ${p.vlan}` : ''}</option
								>{/each}</select
						></label
					><label
						><span>{t('lan.driver')}</span><select class="field" bind:value={draft.driver}
							><option value="ipvlan">ipvlan L2</option><option value="macvlan">macvlan</option
							></select
						><small class="muted">{t('lan.driverHint')}</small></label
					>
				</fieldset>
				<fieldset>
					<legend>{t('lan.addresses')}</legend><label
						><span>{t('lan.subnet')}</span><input
							class="field mono"
							bind:value={draft.subnet}
							required
						/></label
					><label
						><span>{t('netx.gateway')}</span><input
							class="field mono"
							bind:value={draft.gateway}
							required
						/></label
					><label
						><span>{t('lan.range')}</span><input
							class="field mono"
							bind:value={draft.range}
							required
						/><small class="muted">{t('lan.rangeHint')}</small></label
					>
				</fieldset>
			</div>
			<label class="check"
				><input type="checkbox" bind:checked={draft.hostAccess} /><span>{t('lan.hostAccess')}</span
				></label
			>{#if draft.hostAccess}<label
					><span>{t('lan.hostAddress')}</span><input
						class="field mono"
						bind:value={draft.hostAddress}
						required
					/><small class="muted">{t('lan.hostHint')}</small></label
				>{/if}
			<p class="notice-warn">{t('lan.external')}</p>
			<div class="dialog-actions">
				<button class="btn" type="button" disabled={busy} onclick={() => (draft = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy}>{t('lan.add')}</button>
			</div>
		</form></Dialog
	>{/if}
{#if remove}<Dialog title={t('netx.remove')} locked={busy} onclose={() => (remove = null)}
		>{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<p>{t('lan.removeHint', { name: remove.name })}</p>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (remove = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={erase}>{t('netx.remove')}</button>
		</div></Dialog
	>{/if}

<style>
	.content,
	fieldset {
		padding: var(--tile-pad);
	}
	article {
		padding: var(--tile-pad) 0;
		border-bottom: 1px solid var(--border);
	}
	article:last-child {
		border: 0;
	}
	.row,
	.dialog-actions {
		display: flex;
		gap: var(--gap);
		justify-content: space-between;
		align-items: center;
	}
	.row > div {
		min-width: 0;
	}
	.row p,
	.mono {
		overflow-wrap: anywhere;
	}
	form,
	label,
	fieldset {
		display: grid;
		gap: var(--gap);
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap);
	}
	.check {
		display: flex;
		align-items: center;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	.dialog-actions {
		justify-content: flex-end;
		margin-top: var(--gap);
	}
	@media (max-width: 760px) {
		.two {
			grid-template-columns: 1fr;
		}
	}
</style>
