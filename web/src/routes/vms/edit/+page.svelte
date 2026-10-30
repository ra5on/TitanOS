<script lang="ts">
	import { page } from '$app/state';
	import { api, type VM, type VMStatus } from '#lib/api.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import VmForm from '../VmForm.svelte';

	let vm = $state<VM | null>(null);
	let error = $state('');

	$effect(() => {
		const name = page.url.searchParams.get('vm');
		api.get<VMStatus>('/vms').then(
			(res) => {
				vm = res.vms.find((candidate) => candidate.name === name) ?? null;
				if (!vm) error = errorText('vm_not_found');
			},
			(err) => (error = apiErrorText(err)),
		);
	});
</script>

{#if error}
	<p class="notice-error" role="alert">{error} <a href="/vms">{t('vms.title')}</a></p>
{:else if vm}
	{#key vm.name}<VmForm existing={vm} />{/key}
{/if}
