<script lang="ts">
	import type { VM, DockerContainer } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import CollectionTile from './CollectionTile.svelte';
	import type { TileOptions } from '../options.ts';
	let {
		kind,
		name,
		vm,
		container,
		ready,
		error,
		admin,
		options,
		editing = false,
		kvm = false,
		tailscaleHost = '',
		onrefresh = () => {},
	}: {
		kind: 'vm' | 'container';
		name: string;
		vm?: VM;
		container?: DockerContainer;
		ready: boolean;
		error: string;
		admin: boolean;
		options: TileOptions;
		editing?: boolean;
		kvm?: boolean;
		tailscaleHost?: string;
		onrefresh?: () => void;
	} = $props();
	const item = $derived(kind === 'vm' ? vm : container);
</script>

{#if ready && !error && !item}<p class="muted">
		{t('dashboard.workloadMissing', { name })}
	</p>{:else}<CollectionTile
		kind={kind === 'vm' ? 'vms' : 'docker'}
		options={{ ...options, items: null, runningOnly: false }}
		containers={container ? [container] : []}
		vms={vm ? [vm] : []}
		{ready}
		{error}
		{admin}
		{editing}
		{kvm}
		{tailscaleHost}
		{onrefresh}
		standalone
	/>{/if}
