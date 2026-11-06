<script lang="ts">
	// Gruppe „Schutz“ (PLAN 54-S) in the list of functions: Ransomware-
	// Wächter, Notfallhandbuch, Notfallzugang, each off by default. Owners
	// and administrators switch them on; a tap opens the view of the
	// function with its settings below. A registered person sees the
	// emergency access here (to ask), and the handbook while granted.
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Features, FeaturesMine, FeaturesPublic } from '#lib/agent-features.ts';
	import { loadProtectMe, type ProtectMe } from '#lib/features/protect.ts';
	import FeatureRow from './FeatureRow.svelte';
	import ProtectViews from './ProtectViews.svelte';
	import ProtectSettings from './ProtectSettings.svelte';

	type Props = {
		admin: boolean;
		nas: Features | null;
		pub: FeaturesPublic | null;
		mine: FeaturesMine | null;
		savenas: (next: Features) => Promise<void>;
		savemine: (next: FeaturesMine) => Promise<void>;
		calm?: boolean;
	};
	let { admin, nas, pub, savenas, calm = false }: Props = $props();

	type Id = 'guard' | 'handbook' | 'emergency';
	let open = $state<Id | null>(null);
	let me = $state<ProtectMe | null>(null);
	$effect(() => {
		void pub;
		loadProtectMe()
			.then((m) => (me = m))
			.catch(() => (me = null));
	});

	async function toggle(id: Id, on: boolean) {
		if (!nas) return;
		await savenas({ ...nas, [id]: { ...nas[id], enabled: on } });
	}
	const guardSub = $derived(
		t(`agent.f.protect.g.mode.${nas?.guard.mode ?? 'lock'}` as MessageKey) +
			(me?.alerts
				? ' · ' +
					t(me.alerts === 1 ? 'agent.f.protect.g.openAlert' : 'agent.f.protect.g.openAlerts', {
						count: me.alerts,
					})
				: ''),
	);
	const emergencySub = $derived.by(() => {
		if (!admin) {
			const r = me?.request;
			if (r?.state === 'granted') return t('agent.f.protect.e.mineGranted');
			if (r?.state === 'waiting') return t('agent.f.protect.e.mineWaiting');
			return t('agent.f.protect.e.rowAsk');
		}
		const days = nas?.emergency.persons?.[0]?.waitDays ?? 3;
		const base = t('agent.f.protect.e.rowSub', { days });
		return me?.requests
			? base +
					' · ' +
					t(
						me.requests === 1 ? 'agent.f.protect.e.openRequest' : 'agent.f.protect.e.openRequests',
						{ count: me.requests },
					)
			: base;
	});
</script>

{#if admin}
	<div class="fgrp">
		<FeatureRow
			icon="🛡️"
			color="#ef4444"
			title={t('agent.f.protect.guard')}
			sub={guardSub}
			on={!!nas?.guard.enabled}
			ontoggle={(on) => toggle('guard', on)}
			onopen={() => (open = 'guard')}
		/>
		<FeatureRow
			icon="📕"
			color="#6366f1"
			title={t('agent.f.protect.handbook')}
			sub={t('agent.f.protect.h.rowSub')}
			on={!!nas?.handbook.enabled}
			ontoggle={(on) => toggle('handbook', on)}
			onopen={() => (open = 'handbook')}
		/>
		<FeatureRow
			icon="🔑"
			color="#eab308"
			title={t('agent.f.protect.emergency')}
			sub={emergencySub}
			on={!!nas?.emergency.enabled}
			ontoggle={(on) => toggle('emergency', on)}
			onopen={() => (open = 'emergency')}
		/>
	</div>
{:else if me?.emergency || me?.handbook}
	<div class="fgrp">
		{#if me.emergency}
			<FeatureRow
				icon="🔑"
				color="#eab308"
				title={t('agent.f.protect.emergency')}
				sub={emergencySub}
				toggle={false}
				onopen={() => (open = 'emergency')}
			/>
		{/if}
		{#if me.handbook}
			<FeatureRow
				icon="📕"
				color="#6366f1"
				title={t('agent.f.protect.handbook')}
				sub={t('agent.f.protect.h.rowSub')}
				toggle={false}
				onopen={() => (open = 'handbook')}
			/>
		{/if}
	</div>
{:else}
	<div class="fgrp">
		<FeatureRow
			icon="🛡️"
			color="#ef4444"
			title={t('agent.f.protect.guard')}
			sub={t('agent.f.protect.adminOnly')}
			toggle={false}
		/>
	</div>
{/if}

{#if open}
	{#snippet settings()}
		{#if admin && nas}
			<ProtectSettings id={open!} {nas} {savenas} />
		{/if}
	{/snippet}
	<ProtectViews
		id={open}
		{calm}
		onclose={() => (open = null)}
		extra={admin && nas ? settings : undefined}
	/>
{/if}
