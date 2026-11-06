<script lang="ts">
	// The report of the Hardware-Berater (PLAN 54-V, Entwurf 10): a disk
	// that will probably fail on top, then memory, volumes, graphics card,
	// temperatures. Computed at night from values there without waking a
	// disk; "Jetzt prüfen" computes the same now.
	import { apiErrorText, tOr } from '#lib/i18n/index.svelte.ts';
	import { GIB, formatBytes, formatDateTime, formatDecimal } from '#lib/format.ts';
	import {
		findingTone,
		loadAdvisor,
		runAdvisor,
		whatIfOn,
		yearsOf,
		type AdvisorView,
		type Finding,
		type Scenario,
	} from '#lib/features/foresight.ts';
	import { api } from '#lib/api.ts';
	import ForesightSheet from './ForesightSheet.svelte';
	import ForesightWhatIf from './ForesightWhatIf.svelte';

	let { onclose }: { onclose: () => void } = $props();

	const P = 'agent.f.foresight.';
	const tk = (key: string, params?: Record<string, string | number>) => {
		const text = tOr(P + key, key);
		if (!params) return text;
		return text.replace(/\{(\w+)\}/g, (m, n: string) => (n in params ? String(params[n]) : m));
	};

	let view = $state<AdvisorView | null>(null);
	let error = $state('');
	let busy = $state(false);
	let whatIf = $state(false);
	let play = $state<{ scenarios: Scenario[]; start: Scenario } | null>(null);

	async function load() {
		try {
			view = await loadAdvisor();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	void load();
	void whatIfOn().then((s) => (whatIf = s.enabled));

	async function run() {
		busy = true;
		error = '';
		try {
			view = await runAdvisor();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	const findings = $derived(view?.saved?.report.findings ?? []);
	const failing = $derived(findings.filter((f) => f.area === 'disk' && f.level === 'fail'));
	// Healthy disks are not listed one by one; the others are.
	const rows = $derived(
		findings.filter((f) => !(f.area === 'disk' && (f.level === 'fail' || f.level === 'ok'))),
	);
	const icon: Record<Finding['area'], string> = {
		disk: '💽',
		memory: '🧠',
		volume: '📦',
		cpu: '⚙️',
		gpu: '🎮',
		temperature: '🌡️',
	};

	function titleOf(f: Finding) {
		return tk('area.' + f.area, { name: f.subject });
	}
	const n = (v?: number) => formatDecimal(v ?? 0, 1);
	function subOf(f: Finding): string {
		const p: Record<string, string | number> = {
			from: n(f.from),
			to: n(f.to),
			weeks: n(f.weeks),
			percent: n(f.percent),
			days: n(f.days),
			hours: n(f.hours),
			label: f.label ?? '',
			celsius: n(f.celsius),
		};
		let text: string;
		switch (f.code) {
			case 'wornOut':
				text = tk(f.days != null ? 'c.wornOutDays' : 'c.wornOut', p);
				break;
			case 'gpuOK':
			case 'gpuFull': {
				const used = f.bytes != null ? formatBytes(f.bytes) : '';
				const total = formatBytes(f.totalBytes ?? 0);
				text =
					f.bytes == null
						? tk('c.gpuUnknown', { total })
						: tk(f.code === 'gpuFull' ? 'c.gpuFull' : 'c.gpuOllama', { used, total });
				break;
			}
			default:
				text = tk('c.' + f.code, p);
		}
		const parts = [text];
		if (f.suggestBytes)
			parts.push(tk('c.suggest', { bytes: `${Math.round(f.suggestBytes / GIB)} GB` }));
		if (f.area === 'disk' && f.hours) parts.push(tk('age', { years: n(yearsOf(f.hours)) }));
		if (f.also?.length)
			parts.push(tk('also', { list: f.also.map((c) => tk('short.' + c)).join(', ') }));
		return parts.join(' · ');
	}

	async function playDisk(f: Finding) {
		// The volume of the disk, from the storage status the page may read.
		try {
			const st = await api.get<{
				volumes: { name: string; level: string; filesystem: string; members: { disk: string }[] }[];
			}>('/storage');
			const vol = st.volumes.find((v) => v.members.some((m) => m.disk === f.subject));
			const start: Scenario = { kind: 'disk', disk: f.subject, volume: vol?.name };
			play = { scenarios: [start], start };
		} catch (e) {
			error = apiErrorText(e);
		}
	}
</script>

<ForesightSheet title={tk('advisor')} {onclose}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if view && !view.saved}
		<p class="fmuted">{tk('empty')}</p>
	{/if}
	{#each failing as f (f.subject)}
		<div class="fnote bad" data-testid="advisor-fail">
			<b>💽 {tk('failHead', { disk: f.subject })}</b>
			<small>{subOf(f)}</small>
			<div class="fbtns">
				<a class="btn" href="/storage/smart?disk={encodeURIComponent(f.subject)}">{tk('history')}</a
				>
				{#if whatIf}<button type="button" class="btn" onclick={() => playDisk(f)}
						>{tk('playThrough')}</button
					>{/if}
			</div>
			{#if view?.saved?.offers?.[f.subject]?.length}
				<small>{tk('offers')}</small>
				<ul class="offers">
					{#each view.saved.offers[f.subject] as o (o.url)}
						<li>
							<a href={o.url} target="_blank" rel="noopener noreferrer nofollow">{o.title}</a
							>{#if o.price}<span class="price"> · {o.price} €</span>{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/each}
	{#if rows.length}
		<div class="fgrp2" data-testid="advisor-rows">
			{#each rows as f (f.area + f.subject)}
				<div class="frow2">
					<span class="fic" style:--c="#22c55e" aria-hidden="true">{icon[f.area]}</span>
					<span class="ft"><b>{titleOf(f)}</b><small>{subOf(f)}</small></span>
					<span class="ftag {findingTone(f.level)}">{tk('badge.' + f.level)}</span>
				</div>
			{/each}
		</div>
	{/if}
	{#if view?.saved}
		<p class="fmuted">
			{tk('lastRun', {
				time: formatDateTime(new Date(view.saved.lastRun)),
			})}{#if !view.saved.history}
				· {tk('noHistory')}{/if}
		</p>
	{/if}
	<div class="fbtns">
		<button type="button" class="btn" disabled={busy || view?.running} onclick={run}
			>{busy || view?.running ? tk('running') : tk('run')}</button
		>
	</div>
	{#if view?.settings.prices && !view.web}<p class="fmuted">{tk('set.pricesNoWeb')}</p>{/if}
	<p class="fmuted">
		{tk('footer')}{#if view && !view.settings.prices}
			{' '}{tk('footerNoPrices')}{/if}
	</p>
</ForesightSheet>
{#if play}
	<ForesightWhatIf scenarios={play.scenarios} start={play.start} onclose={() => (play = null)} />
{/if}

<style>
	.offers {
		margin: 0;
		padding-left: 18px;
		font-size: 13px;
		overflow-wrap: anywhere;
	}
	.price {
		font-variant-numeric: tabular-nums;
	}
</style>
