<script lang="ts">
	// The way to one node, as an exception to the settings of the cluster
	// (PUT /cluster/nodes/{id}/route); "Use the cluster setting" removes it.
	import { untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		WAYS,
		wayLabel,
		type ClusterPage,
		type NodeView,
		type Route,
		type Ways,
	} from '#lib/cluster.svelte.ts';

	type Props = {
		node: NodeView;
		route: Route | undefined;
		ways: Ways;
		onclose: () => void;
		onsaved: (page: ClusterPage) => void;
	};
	let { node, route, ways, onclose, onsaved }: Props = $props();

	// The dialog edits a copy of the exception it opened with.
	let manage = $state(untrack(() => route?.manage ?? ''));
	let only = $state(untrack(() => !!route?.only));
	let backup = $state(untrack(() => route?.backup ?? ''));
	let busy = $state(false);
	let error = $state('');

	async function save(next: Route) {
		busy = true;
		error = '';
		try {
			onsaved(
				await api.put<ClusterPage>(
					'/cluster/nodes/' + encodeURIComponent(node.id) + '/route',
					next,
				),
			);
			onclose();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('cluster.routeTitle', { name: node.name })} {onclose} locked={busy}>
	<div class="rows">
		<FormRow
			label={t('cluster.routeManage')}
			id="route-manage"
			hint={t('cluster.routeManageHint', { way: wayLabel(ways.manage) })}
		>
			<div class="line">
				<select id="route-manage" class="field" bind:value={manage}>
					<option value="">{t('cluster.routeLikeCluster')}</option>
					{#each WAYS as way (way)}<option value={way}>{wayLabel(way)}</option>{/each}
				</select>
			</div>
		</FormRow>
		<FormRow label={t('cluster.onlyWay')}>
			<Toggle
				bind:checked={only}
				label={t('cluster.onlyWay')}
				disabled={!manage}
				text={t(only ? 'cluster.onlyOn' : 'cluster.onlyOff')}
			/>
		</FormRow>
		<FormRow label={t('cluster.routeBackup')} id="route-backup">
			<div class="line">
				<select id="route-backup" class="field" bind:value={backup}>
					<option value="">{t('cluster.routeLikeCluster')}</option>
					{#each WAYS as way (way)}<option value={way}
							>{t('cluster.onlyThis', { way: wayLabel(way) })}</option
						>{/each}
				</select>
			</div>
		</FormRow>
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions spread">
		<button type="button" class="btn" disabled={busy || !route} onclick={() => save({})}
			>{t('cluster.routeReset')}</button
		>
		<span class="gap"></span>
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button
			type="button"
			class="btn btn-primary"
			disabled={busy}
			onclick={() =>
				save({
					manage: manage as Route['manage'],
					only: !!manage && only,
					backup: backup as Route['backup'],
				})}>{t('cluster.routeApply')}</button
		>
	</div>
</Dialog>

<style>
	.rows {
		margin: 0 calc(-1 * var(--space-4));
	}
	.rows :global(.form-row) {
		grid-template-columns: minmax(8rem, 11rem) minmax(0, 1fr);
	}
	.spread .gap {
		flex: 1;
	}
</style>
