<script lang="ts">
	// "Durchspielen" (PLAN 54-V, Entwurf 09): what happens if a disk fails,
	// a volume runs full or another level were chosen. Computed on the
	// server from what it holds in RAM; nothing on the NAS changes. Advice
	// only links to existing pages; "Starten" starts a backup job the person
	// picked, through the backup's own route.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import {
		formatDateTime,
		formatDecimal,
		formatDiskBytes,
		formatBytes,
		formatDuration,
		formatMegabytesPerSecond,
	} from '#lib/format.ts';
	import {
		outcomeTone,
		playWhatIf,
		scenarioKey,
		yearsOf,
		type Advice,
		type Reason,
		type Scenario,
		type WhatIfResult,
	} from '#lib/features/foresight.ts';
	import ForesightSheet from './ForesightSheet.svelte';

	type Props = { scenarios: Scenario[]; start: Scenario; onclose: () => void };
	let { scenarios, start, onclose }: Props = $props();

	const P = 'agent.f.foresight.';
	const tk = (key: string, params?: Record<string, string | number>) => {
		const text = tOr(P + key, key);
		if (!params) return text;
		return text.replace(/\{(\w+)\}/g, (m, n: string) => (n in params ? String(params[n]) : m));
	};
	const levelName = (l?: string) => (l ? tOr(P + 'level.' + l, l) : '');

	let current = $state<string>('');
	$effect.pre(() => {
		if (!current) current = scenarioKey(start);
	});
	const scenario = $derived(scenarios.find((s) => scenarioKey(s) === current) ?? start);
	let result = $state<WhatIfResult | null>(null);
	let error = $state('');
	let started = $state<Record<string, boolean>>({});

	$effect(() => {
		const sc = scenario;
		const key = scenarioKey(sc);
		result = null;
		error = '';
		// A late answer for a scenario no longer chosen is dropped.
		playWhatIf(sc)
			.then((r) => {
				if (key === current) result = r;
			})
			.catch((e) => {
				if (key === current) error = apiErrorText(e);
			});
	});

	function optionLabel(s: Scenario) {
		if (s.kind === 'disk') return tk('opt.disk', { disk: s.disk ?? '' });
		if (s.kind === 'full') return tk('opt.full', { volume: s.volume ?? '' });
		return tk('opt.level', { level: levelName(s.level) });
	}
	const title = $derived(
		scenario.kind === 'disk'
			? tk('title.disk', { disk: scenario.disk ?? '' })
			: scenario.kind === 'full'
				? tk('title.full', { volume: scenario.volume ?? '' })
				: tk('title.level', { volume: scenario.volume ?? '', level: levelName(scenario.level) }),
	);

	function headline(r: WhatIfResult): { head: string; sub: string } {
		const p = {
			volume: r.volume ?? '',
			disk: r.disk ? r.disk + (r.serial ? ` (${r.serial})` : '') : '',
			level: levelName(r.level),
			n: r.disks ?? 0,
			bytes: formatDiskBytes(r.dataAtRiskBytes),
		};
		if (r.full) {
			const f = r.full;
			const free = formatDiskBytes(f.freeBytes);
			return {
				head: tk('full.head', { volume: p.volume, pct: Math.round(f.usedPct) }),
				sub:
					f.daysToFull != null && f.dailyBytes
						? tk('full.days', {
								perDay: formatBytes(f.dailyBytes),
								days: Math.round(f.daysToFull),
								free,
							})
						: tk('full.noGrowth', { free }),
			};
		}
		if (r.compare) {
			const c = r.compare;
			return {
				head: tk('lv.head', { to: levelName(c.to.level), from: levelName(c.from.level) }),
				sub: c.way === 'profile' ? tk('lv.profile') : tk('lv.newVolume'),
			};
		}
		const o = r.outcome ?? 'redundant';
		return { head: tk('o.' + o, p), sub: tk('o.' + o + 'Sub', p) };
	}

	function reasonText(x: Reason) {
		const p = {
			disk: x.disk ?? '',
			value: x.value ?? 0,
			years: formatDecimal(yearsOf(x.value ?? 0), 1),
		};
		return tk('r.' + x.code, p);
	}

	type Act = { href?: string; run?: string };
	function adviceOf(a: Advice): { title: string; sub?: string; act?: Act } {
		const size = a.bytes ? formatDiskBytes(a.bytes) : '';
		switch (a.code) {
			case 'startBackup':
				return { title: tk('a.startBackup', { share: a.share ?? '' }), act: { run: a.job } };
			case 'createBackup':
				return { title: tk('a.createBackup', { share: a.share ?? '' }), act: { href: '/backups' } };
			case 'replacement':
				return {
					title: tk('a.replacement'),
					sub: tk(a.kind === 'hdd' ? 'a.replacementCMR' : 'a.replacementSub', { size }),
				};
			case 'restore':
				return { title: tk('a.restore'), act: { href: '/backups' } };
			case 'checkCable':
				return { title: tk('a.checkCable', { disk: a.disk ?? '' }), sub: tk('a.checkCableSub') };
			case 'addSpare':
				return { title: tk('a.addSpare'), sub: tk('a.addSpareSub'), act: { href: '/storage' } };
			case 'addDisks':
				return {
					title: tk('a.addDisks', { count: a.count ?? 0, level: levelName(a.level) }),
				};
			case 'mirror':
			case 'grow':
			case 'cleanSnapshots':
			case 'checkUsage':
			case 'cacheReadOnly':
			case 'profile':
			case 'newVolume':
				return { title: tk('a.' + a.code), act: { href: '/storage' } };
		}
		return { title: a.code };
	}

	async function startBackup(id: string) {
		try {
			await api.post(`/backups/${encodeURIComponent(id)}/run`);
			started = { ...started, [id]: true };
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const when = (iso: string) => formatDateTime(new Date(iso));
</script>

<ForesightSheet {title} {onclose}>
	{#if scenarios.length > 1}
		<label class="pick">
			<span class="fmuted">{tk('pick')}</span>
			<select class="field" bind:value={current}>
				{#each scenarios as s (scenarioKey(s))}
					<option value={scenarioKey(s)}>{optionLabel(s)}</option>
				{/each}
			</select>
		</label>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if !result && !error}
		<p class="fmuted" aria-live="polite">{tk('loading')}</p>
	{:else if result}
		{@const r = result}
		{@const h = headline(r)}
		<div class="fnote {outcomeTone(r)}" data-testid="whatif-head">
			<b>{h.head}</b>
			<small>{h.sub}</small>
		</div>
		<div class="fgrp2">
			{#if r.rebuild}
				{@const rb = r.rebuild}
				<div class="frow2">
					<span class="fic" style:--c="#06b6d4" aria-hidden="true">⏱️</span>
					<span class="ft"
						><b>{tk('rebuild')}</b><small
							>{tk(r.filesystem === 'btrfs' ? 'rebuildBtrfs' : 'rebuildSub', {
								size: formatDiskBytes(rb.bytes),
								time: formatDuration(rb.seconds),
								how: tk(rb.measured ? 'measured' : 'estimated'),
								speed: formatMegabytesPerSecond(rb.bps),
							})}{#if rb.auto}<br />{tk('auto')}{/if}</small
						></span
					>
				</div>
			{/if}
			{#if r.risk}
				{@const risk = r.risk}
				<div class="frow2">
					<span class="fic" style:--c="#fbbf24" aria-hidden="true">⚠️</span>
					<span class="ft"
						><b>{tk('risk')}</b><small
							>{risk.reasons.length
								? risk.reasons.map(reasonText).join(' · ')
								: tk('r.none')}</small
						></span
					>
					<span
						class="ftag {risk.level === 'normal' ? 'ok' : risk.level === 'high' ? 'bad' : 'warn'}"
						>{tk('risk.' + risk.level)}</span
					>
				</div>
			{/if}
			{#if r.full}
				<div class="frow2">
					<span class="fic" style:--c="#ef4444" aria-hidden="true">📦</span>
					<span class="ft"
						><b>{tk('full.effects')}</b><small
							>{r.full.effects.map((e) => tk('e.' + e)).join(' · ')}</small
						></span
					>
				</div>
			{/if}
			{#if r.compare}
				{@const c = r.compare}
				<div class="frow2">
					<span class="fic" style:--c="#06b6d4" aria-hidden="true">🛡️</span>
					<span class="ft"
						><b>{tk('lv.tolerance')}</b><small
							>{tk('lv.toleranceSub', {
								a: c.from.tolerates,
								b: c.to.tolerates,
								to: levelName(c.to.level),
							})}</small
						></span
					>
				</div>
				<div class="frow2">
					<span class="fic" style:--c="#22c55e" aria-hidden="true">📦</span>
					<span class="ft"
						><b>{tk('lv.space')}</b><small
							>{tk('lv.spaceSub', {
								from: formatDiskBytes(c.from.usableBytes),
								to: formatDiskBytes(c.to.usableBytes),
							})}{#if !c.fits}
								· {tk('lv.noFit', { used: formatDiskBytes(c.usedBytes) })}{/if}</small
						></span
					>
					<span class="ftag {c.fits ? 'ok' : 'warn'}"
						>{c.fits ? tk('badge.ok') : tk('badge.tight')}</span
					>
				</div>
			{/if}
			{#each r.gaps as g (g.share)}
				<div class="frow2">
					<span class="fic" style:--c="#ef4444" aria-hidden="true">🗂️</span>
					<span class="ft"
						><b>{tk('gap')}</b><small
							>{g.never
								? tk('gap.never', { share: g.share })
								: g.unknown
									? tk('gap.unknown', { share: g.share })
									: g.notYet
										? tk('gap.notYet', { share: g.share, job: g.job ?? '' })
										: tk('gap.days', {
												share: g.share,
												days: g.days ?? 0,
												size: g.bytes ? ` (${formatDiskBytes(g.bytes)})` : '',
											})}</small
						></span
					>
					<span class="ftag {g.unknown ? 'warn' : 'bad'}">{tk('gapBadge')}</span>
				</div>
			{/each}
			{#if r.safe.length}
				<div class="frow2">
					<span class="fic" style:--c="#34d399" aria-hidden="true">✔️</span>
					<span class="ft"
						><b>{tk('safe')}</b><small
							>{r.safe
								.map((s) => tk('safeItem', { share: s.share, time: when(s.at) }))
								.join(', ')}</small
						></span
					>
					<span class="ftag ok">{tk('badge.ok')}</span>
				</div>
			{/if}
		</div>
		{#if r.filesystem === 'btrfs' && r.scenario.kind === 'disk'}
			<p class="fmuted">{tk('snapshots')}</p>
		{/if}
		{#if r.advice.length}
			<div class="fh">{tk('advice')}</div>
			<div class="fgrp2">
				{#each r.advice as a, i (i)}
					{@const x = adviceOf(a)}
					<div class="frow2">
						<span class="ft"
							><b>{x.title}</b>{#if x.sub}<small>{x.sub}</small>{/if}</span
						>
						{#if x.act?.run}
							{@const job = x.act.run}
							<button
								type="button"
								class="btn btn-primary"
								disabled={started[job]}
								onclick={() => startBackup(job)}
								>{started[job] ? tk('a.started') : tk('a.start')}</button
							>
						{:else if x.act?.href}
							<a class="btn" href={x.act.href}>{tk('a.open')}</a>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
		<p class="fmuted">{tk('footerWhatIf')}</p>
	{/if}
</ForesightSheet>

<style>
	.pick {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.pick select {
		width: 100%;
		min-width: 0;
	}
</style>
