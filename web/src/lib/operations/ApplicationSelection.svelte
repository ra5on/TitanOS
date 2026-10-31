<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type DockerStatus, type VMStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { operationError } from '#lib/operations.ts';
	export type Application = { kind: string; name: string; method: string };
	let {
		applications = $bindable([]),
		allowStop = $bindable(false),
	}: { applications?: Application[]; allowStop?: boolean } = $props();
	let options = $state<{ kind: string; name: string }[]>([]),
		choice = $state(''),
		error = $state('');
	onMount(() => {
		Promise.allSettled([api.get<DockerStatus>('/docker'), api.get<VMStatus>('/vms')]).then(
			([docker, vms]) => {
				options = [
					...(docker.status === 'fulfilled'
						? docker.value.containers.map((c) => ({ kind: 'docker', name: c.name }))
						: []),
					...(vms.status === 'fulfilled'
						? vms.value.vms.map((v) => ({ kind: 'vm', name: v.name }))
						: []),
				];
				if (docker.status === 'rejected') error = operationError(docker.reason, t);
				if (vms.status === 'rejected') error = operationError(vms.reason, t);
			},
		);
	});
	function add() {
		const [kind, ...rest] = choice.split(':');
		const name = rest.join(':');
		if (!name || applications.some((a) => a.kind === kind && a.name === name)) return;
		applications = [...applications, { kind, name, method: 'stop' }];
		choice = '';
	}
</script>

<fieldset>
	<legend>{t('ops.appBackups')}</legend>
	<p class="muted">{t('ops.appHint')}</p>
	{#if error}<p class="notice-warn">{error}</p>{/if}
	<div class="select">
		<select class="field" aria-label={t('ops.appAdd')} bind:value={choice}
			><option value="">{t('ops.choose')}</option
			>{#each options as o (`${o.kind}:${o.name}`)}<option value={`${o.kind}:${o.name}`}
					>{o.kind === 'vm' ? 'VM' : 'Docker'} · {o.name}</option
				>{/each}</select
		><button class="btn" type="button" disabled={!choice || applications.length >= 50} onclick={add}
			>{t('ops.appAdd')}</button
		>
	</div>
	{#each applications as a, i (`${a.kind}:${a.name}`)}<div class="application">
			<span>{a.kind === 'vm' ? 'VM' : 'Docker'} · <strong>{a.name}</strong></span><select
				class="field"
				aria-label={a.name}
				bind:value={a.method}
				><option value="stop">{t('ops.appStop')}</option>{#if a.kind === 'docker'}<option
						value="postgres-export">{t('ops.pgExport')}</option
					>{/if}</select
			><button
				class="btn"
				type="button"
				onclick={() => (applications = applications.filter((_, n) => n !== i))}
				>{t('ops.remove')}</button
			>
		</div>{/each}
	{#if applications.some((a) => a.method === 'stop')}<label class="check"
			><input type="checkbox" bind:checked={allowStop} required />{t('ops.allowStop')}</label
		>{/if}
</fieldset>

<style>
	fieldset {
		padding: 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	.select,
	.application {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		align-items: center;
		margin: 12px 0;
	}
	.select select,
	.application select {
		width: auto;
		flex: 1 1 200px;
	}
	.application span {
		overflow-wrap: anywhere;
		font-size: var(--text-base);
	}
	.check {
		display: flex;
		gap: 8px;
		align-items: center;
		font-size: var(--text-base);
	}
	legend {
		font-weight: 600;
	}
</style>
