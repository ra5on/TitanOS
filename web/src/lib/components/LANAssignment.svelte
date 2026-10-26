<script lang="ts">
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { assignContainerLAN, lanAssignment, type ContainerLAN } from '#lib/compose/lan.ts';
	let {
		text,
		service,
		onchange,
	}: { text: string; service: string; onchange: (text: string) => void } = $props();
	let networks = $state<ContainerLAN[]>([]),
		selected = $state(''),
		address = $state(''),
		error = $state('');
	$effect(() => {
		api
			.get<{ networks: ContainerLAN[] }>('/docker/networks')
			.then((r) => (networks = r.networks ?? []))
			.catch(() => {});
	});
	$effect(() => {
		const value = lanAssignment(text, service);
		selected = value.network;
		address = value.address;
	});
	function assign() {
		try {
			const next = assignContainerLAN(text, service, selected, address);
			error = '';
			onchange(next);
		} catch {
			error = t('lan.assignError');
		}
	}
</script>

<fieldset>
	<legend>{t('lan.service')}</legend><label
		><span>{t('lan.title')}</span><select class="field" bind:value={selected}
			><option value="">{t('lan.noLAN')}</option>{#each networks as n}<option value={n.name}
					>{n.name} · {n.range}</option
				>{/each}</select
		></label
	>{#if selected}<label
			><span>{t('lan.staticAddress')}</span><input class="field mono" bind:value={address} /><small
				class="muted">{t('lan.autoHint')}</small
			></label
		>{/if}<button type="button" class="btn" onclick={assign}>{t('lan.assignDraft')}</button>
	<p class="muted">{t('lan.privateHint')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}<a href="/docker/networks"
		>{t('lan.manage')}</a
	>
</fieldset>

<style>
	fieldset {
		display: grid;
		gap: var(--gap);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: var(--tile-pad);
		min-width: 0;
	}
	label {
		display: grid;
		gap: 8px;
	}
</style>
