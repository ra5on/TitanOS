<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	let { address }: { address: string } = $props();
	let percent = $state(0);
	let error = $state('');
	let ready = $state(false);
	let busy = $state(false);
	let saved = $state(false);
	onMount(() => {
		api
			.get<{
				settings: Record<string, { powerPercent: number }>;
				problems: Record<string, string>;
			}>('/hardware/nvidia')
			.then(
				(value) => {
					percent = value.settings[address]?.powerPercent ?? 0;
					error = value.problems[address] ?? '';
					ready = true;
				},
				(err) => (error = apiErrorText(err)),
			);
	});
	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			await api.put(`/hardware/nvidia/${address}`, { powerPercent: percent });
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: a part of the card's row; the choice is stored when it changes. -->
<SettingSub
	title={t('hardware.powerLimit')}
	for="nvidia-power-{address}"
	desc={saved ? t('hardware.powerSaved') : undefined}
>
	{#snippet control()}
		<select
			id="nvidia-power-{address}"
			class="field"
			bind:value={percent}
			disabled={!ready || busy}
			onchange={save}
		>
			<option value={0}>{t('hardware.powerDefault')}</option>
			{#each [20, 30, 40, 50, 60, 70, 80, 90, 100] as value (value)}<option {value}
					>{value} %</option
				>{/each}
		</select>
	{/snippet}
</SettingSub>
{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
