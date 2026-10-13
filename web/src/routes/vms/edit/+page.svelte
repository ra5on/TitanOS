<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError, type VM, type VMStatus } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
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
			(err) => (error = errorText(err instanceof ApiError ? err.code : 'internal'))
		);
	});
</script>

{#if error}
	<p class="notice-error" role="alert">{error} <a href="/vms">{t('vms.title')}</a></p>
{:else if vm}
	{#key vm.name}<VmForm existing={vm} />{/key}
{/if}
