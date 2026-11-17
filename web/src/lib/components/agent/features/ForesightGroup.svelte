<script lang="ts">
	// Gruppe „Vorausschau“ (PLAN 54-V) in the list of functions: Was-wäre-wenn
	// and Hardware-Berater, NAS-wide, switched by owners and administrators,
	// off by default. A tap opens the settings of the function.
	import { ApiError } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { Features, FeaturesMine, FeaturesPublic } from '#lib/agent-features.ts';
	import { forgetWhatIf, type Advisor } from '#lib/features/foresight.ts';
	import FeatureRow from './FeatureRow.svelte';
	import ForesightSheet from './ForesightSheet.svelte';
	import ForesightAdvisor from './ForesightAdvisor.svelte';

	type Props = {
		admin: boolean;
		nas: Features | null;
		pub: FeaturesPublic | null;
		mine: FeaturesMine | null;
		savenas: (next: Features) => Promise<void>;
		savemine: (next: FeaturesMine) => Promise<void>;
		calm?: boolean;
	};
	let { admin, nas, pub, savenas }: Props = $props();

	let open = $state<'whatIf' | 'advisor' | 'report' | null>(null);
	let saving = $state(false);
	let at = $state('');

	const whatIfOn = $derived(admin ? !!nas?.whatIf.enabled : !!pub?.whatIf);
	const advisorOn = $derived(admin ? !!nas?.advisor.enabled : !!pub?.advisor);
	const adv = $derived<Required<Advisor>>({
		enabled: !!nas?.advisor.enabled,
		predict: nas?.advisor.predict ?? true,
		bottlenecks: nas?.advisor.bottlenecks ?? true,
		prices: !!nas?.advisor.prices,
		monthly: !!nas?.advisor.monthly,
		at: nas?.advisor.at || '03:30',
	});

	// A failed save is said in the open sheet too: the panel's error line
	// lies behind it (Durchsicht 102, A-32).
	let saveError = $state('');
	async function save(change: Partial<Features>) {
		if (!nas) return;
		saving = true;
		saveError = '';
		try {
			await savenas({ ...nas, ...change });
			forgetWhatIf();
		} catch (e) {
			saveError =
				e instanceof ApiError && e.code === 'agent_features_changed'
					? t('agent.f.changedLost')
					: apiErrorText(e);
		} finally {
			saving = false;
		}
	}
	function closeSheet(next: typeof open = null) {
		saveError = '';
		open = next;
	}
	const setAdvisor = (change: Partial<Advisor>) => save({ advisor: { ...adv, ...change } });
	const status = (on: boolean) => (on ? t('agent.f.foresight.on') : t('agent.f.foresight.off'));
</script>

<div class="fgrp">
	<FeatureRow
		icon="🧪"
		color="#06b6d4"
		title={t('agent.f.foresight.whatIf')}
		sub={admin ? t('agent.f.foresight.whatIfSub') : status(whatIfOn)}
		on={whatIfOn}
		toggle={admin && !!nas}
		ontoggle={(on) => save({ whatIf: { enabled: on } })}
		onopen={admin ? () => (open = 'whatIf') : undefined}
	/>
	<FeatureRow
		icon="🩺"
		color="#22c55e"
		title={t('agent.f.foresight.advisor')}
		sub={admin ? t('agent.f.foresight.advisorSub') : status(advisorOn)}
		on={advisorOn}
		toggle={admin && !!nas}
		ontoggle={(on) => setAdvisor({ enabled: on })}
		onopen={admin ? () => ((at = adv.at), (open = 'advisor')) : undefined}
	/>
</div>

{#if open === 'whatIf'}
	<ForesightSheet title={t('agent.f.foresight.whatIf')} onclose={() => closeSheet()}>
		{#if saveError}<p class="notice-error" role="alert">{saveError}</p>{/if}
		<div class="fgrp">
			<FeatureRow
				icon="🧪"
				color="#06b6d4"
				title={t('agent.f.foresight.whatIf')}
				sub={status(whatIfOn)}
				on={whatIfOn}
				ontoggle={(on) => save({ whatIf: { enabled: on } })}
			/>
		</div>
		<p class="fmuted">{t('agent.f.foresight.whatIfIntro')}</p>
		<p class="fmuted">{t('agent.f.foresight.whatIfWhere')}</p>
		<p class="fmuted">{t('agent.f.foresight.whatIfData')}</p>
		<div class="fbtns">
			<a class="btn" href="/storage">{t('agent.f.foresight.openStorage')}</a>
		</div>
	</ForesightSheet>
{:else if open === 'advisor'}
	<ForesightSheet title={t('agent.f.foresight.advisor')} onclose={() => closeSheet()}>
		{#if saveError}<p class="notice-error" role="alert">{saveError}</p>{/if}
		<div class="fgrp">
			<FeatureRow
				icon="🩺"
				color="#22c55e"
				title={t('agent.f.foresight.advisor')}
				sub={status(advisorOn)}
				on={advisorOn}
				ontoggle={(on) => setAdvisor({ enabled: on })}
			/>
		</div>
		<div class="fgrp" class:dim={!advisorOn}>
			<FeatureRow
				icon="💽"
				color="#3b82f6"
				title={t('agent.f.foresight.set.predict')}
				sub={t('agent.f.foresight.set.predictSub')}
				on={adv.predict}
				ontoggle={(on) => setAdvisor({ predict: on })}
			/>
			<FeatureRow
				icon="🧠"
				color="#22c55e"
				title={t('agent.f.foresight.set.bottlenecks')}
				sub={t('agent.f.foresight.set.bottlenecksSub')}
				on={adv.bottlenecks}
				ontoggle={(on) => setAdvisor({ bottlenecks: on })}
			/>
			<FeatureRow
				icon="🏷️"
				color="#f97316"
				title={t('agent.f.foresight.set.prices')}
				sub={t('agent.f.foresight.set.pricesSub')}
				on={adv.prices}
				ontoggle={(on) => setAdvisor({ prices: on })}
			/>
			<FeatureRow
				icon="🗓️"
				color="#a855f7"
				title={t('agent.f.foresight.set.monthly')}
				sub={t('agent.f.foresight.set.monthlySub')}
				on={adv.monthly}
				ontoggle={(on) => setAdvisor({ monthly: on })}
			/>
		</div>
		<label class="at">
			<span>{t('agent.f.foresight.set.at')}</span>
			<input
				type="time"
				class="field"
				bind:value={at}
				disabled={saving}
				onchange={() => at && at !== adv.at && setAdvisor({ at })}
			/>
			<small class="fmuted">{t('agent.f.foresight.set.atHint')}</small>
		</label>
		{#if advisorOn}
			<div class="fbtns">
				<button type="button" class="btn" onclick={() => closeSheet('report')}
					>{t('agent.f.foresight.openReport')}</button
				>
			</div>
		{/if}
	</ForesightSheet>
{:else if open === 'report'}
	<ForesightAdvisor onclose={() => (open = 'advisor')} />
{/if}

<style>
	.dim {
		opacity: 0.6;
	}
	.at {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.at input {
		max-width: 10rem;
	}
	/* Inside a sheet the group look of FeaturesPanel is not around. */
	:global(.fsheet) .fgrp {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
</style>
