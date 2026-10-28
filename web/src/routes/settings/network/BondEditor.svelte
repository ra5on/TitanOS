<script lang="ts">
	// Adds or edits a bond of the draft: its ports and how they work together.
	import { t } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import {
		bondCandidates,
		bondModeKeys,
		portUsers,
		type NetworkBond,
		type NetworkConfig,
		type NetworkPort,
	} from '#lib/network.ts';

	let {
		bond,
		isNew,
		config,
		ports,
		modes,
		onsave,
		onclose,
	}: {
		bond: NetworkBond;
		isNew: boolean;
		config: NetworkConfig;
		ports: NetworkPort[];
		modes: string[];
		onsave: (b: NetworkBond) => void;
		onclose: () => void;
	} = $props();

	// svelte-ignore state_referenced_locally
	let b = $state($state.snapshot(bond));
	const candidates = $derived(bondCandidates(config, ports, b));
	// A port that carries a network directly cannot join a bond.
	const direct = (port: string) =>
		portUsers(config, port)
			.filter((u) => u.kind === 'network')
			.map((u) => u.name);
	const idOk = $derived(
		/^[a-z][a-z0-9-]{0,9}$/.test(b.id) &&
			!(isNew && (config.bonds ?? []).some((o) => o.id === b.id)),
	);
	const label = (mode: string) =>
		mode in bondModeKeys ? t(bondModeKeys[mode as keyof typeof bondModeKeys]) : mode;

	function save(event: SubmitEvent) {
		event.preventDefault();
		if (!idOk || b.ports.length < 2) return;
		onsave({
			...$state.snapshot(b),
			ports: ports.map((p) => p.name).filter((p) => b.ports.includes(p)),
		});
	}
</script>

<Dialog title={isNew ? t('netx.addBond') : t('netx.bondEdit')} {onclose}>
	<form class="panel editor" onsubmit={save}>
		<label class="stack"
			><span>{t('netx.bondName')}</span><input
				class="field mono"
				bind:value={b.id}
				disabled={!isNew}
				required
				maxlength="10"
				spellcheck="false"
				autocapitalize="none"
			/><small class="muted">{t('netx.bondNameHint')}</small></label
		>
		<label class="stack"
			><span>{t('netx.bondMode')}</span><select class="field" bind:value={b.mode}
				>{#each modes as mode (mode)}<option value={mode}>{label(mode)}</option>{/each}</select
			></label
		>
		<fieldset>
			<legend>{t('netx.ports')}</legend>
			{#each candidates as p (p.name)}
				{@const users = direct(p.name)}
				<label class="check"
					><input
						type="checkbox"
						bind:group={b.ports}
						value={p.name}
						disabled={users.length > 0 && !b.ports.includes(p.name)}
					/><span class="mono">{p.name}</span>{#if users.length}<span class="muted"
							>· {t('netx.portUsedBy', { names: users.join(', ') })}</span
						>{/if}</label
				>
			{/each}
		</fieldset>
		{#if b.ports.length < 2}<p class="muted">{t('netx.bondTwoPorts')}</p>{/if}
		<p class="muted">{t('netx.switchHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button><button
				class="btn btn-primary"
				disabled={!idOk || b.ports.length < 2}>{t('netx.takeOver')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	.editor {
		padding: 0;
	}
	small {
		font-size: 0.75rem;
	}
</style>
