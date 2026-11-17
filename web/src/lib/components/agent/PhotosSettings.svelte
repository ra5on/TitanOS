<script lang="ts">
	// 1.07: the account's own Immich key for the agent (photos_search).
	// Every account sets its own; the NAS checks it with Immich, keeps it
	// encrypted and never sends it back (api/agent_photos.go). Without a key
	// or without Immich the agent has no photo tool.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import ImmichGuide from '../ImmichGuide.svelte';

	type View = {
		immich: boolean;
		keySet: boolean;
		userName?: string;
		port?: number;
		missing?: string[];
		version?: string;
		smart?: boolean;
	};
	let view = $state<View | null>(null);
	let failed = $state('');
	let key = $state('');
	let busy = $state(false);
	let saved = $state('');
	let error = $state('');

	async function load() {
		try {
			view = await api.get<View>('/agent/photos');
			failed = '';
		} catch (e) {
			failed = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!key.trim() || busy) return;
		busy = true;
		error = saved = '';
		try {
			view = await api.put<View>('/agent/photos', { key: key.trim() });
			key = '';
			saved = t('agent.photos.saved', { name: view.userName ?? '' });
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function check() {
		busy = true;
		error = saved = '';
		try {
			view = await api.post<View>('/agent/photos/check');
			saved = t('agent.photos.checked', { name: view.userName ?? '' });
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function remove() {
		busy = true;
		error = saved = '';
		try {
			view = await api.delete<View>('/agent/photos');
			saved = t('agent.photos.removed');
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<p class="hint">{t('agent.photos.intro')}</p>
{#if failed}
	<p class="notice-warn" role="alert">{failed}</p>
{:else if !view}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<div class="grp">
		<div class="row">
			<span class="t"
				><strong>{t('agent.photos.immich')}</strong><small
					>{view.immich
						? t('agent.photos.immichOn', { port: view.port ?? 0 })
						: t('agent.photos.immichOff')}</small
				></span
			>
			{#if !view.immich}<a class="btn small" href="/apps/install/titanos/immich"
					>{t('agent.photos.install')}</a
				>{/if}
		</div>
		{#if view.keySet}
			<div class="row">
				<span class="t"
					><strong>{t('agent.photos.connected')}</strong><small
						>{t('agent.photos.connectedAs', { name: view.userName || '–' })}</small
					></span
				>
			</div>
		{/if}
		{#if view.immich}
			<form class="row col" onsubmit={save}>
				<label class="t" for="immich-key"
					><strong>{view.keySet ? t('agent.photos.keyNew') : t('agent.photos.key')}</strong><small
						>{t('agent.photos.keyHow')}</small
					></label
				>
				<input
					id="immich-key"
					class="field"
					type="password"
					autocomplete="off"
					spellcheck="false"
					maxlength="200"
					bind:value={key}
				/>
				<div class="actions">
					<button type="submit" class="btn btn-primary" disabled={busy || !key.trim()}
						>{t('agent.photos.save')}</button
					>
					{#if view.keySet}
						<button type="button" class="btn" disabled={busy} onclick={check}
							>{t('agent.photos.check')}</button
						>
						<button type="button" class="btn btn-danger-outline" disabled={busy} onclick={remove}
							>{t('agent.photos.remove')}</button
						>
					{/if}
				</div>
			</form>
		{/if}
	</div>
	{#if saved}<p class="notice-ok" role="status">{saved}</p>{/if}
	{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
	{#if view.missing?.length}
		<p class="notice-warn">{t('agent.photos.missing', { list: view.missing.join(', ') })}</p>
	{/if}
	{#if view.smart === false}
		<p class="notice-warn">{t('agent.photos.noSmart')}</p>
	{/if}
	<p class="hint">{t('agent.photos.privacy')}</p>
{/if}
<ImmichGuide />

<style>
	.hint,
	.muted {
		margin: 0 0 var(--space-2);
		line-height: 1.45;
		color: var(--muted);
	}
	.grp {
		display: flex;
		flex-direction: column;
		gap: 3px;
		margin-bottom: var(--space-2);
	}
	.row {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-3);
		min-height: 48px;
		padding: var(--space-2-5) var(--space-4);
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		background: var(--srow-bg);
	}
	.row.col {
		align-items: stretch;
		flex-direction: column;
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.t strong {
		font-weight: 500;
	}
	.t small {
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.field {
		width: 100%;
		min-height: 44px;
		font-size: 16px;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.actions .btn,
	.small {
		min-height: 44px;
	}
</style>
