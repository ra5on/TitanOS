<script lang="ts">
	// Notfallhandbuch (PLAN 54-S.3, mockup 07): the build of the NAS and
	// "Was tun, wenn …" with PDF download. Never passwords or keys.
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { formatDate, formatTime } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import { ApiError } from '#lib/api.ts';
	import {
		downloadHandbook,
		loadHandbook,
		refreshHandbook,
		type HandbookView,
		type NamedFact,
	} from '#lib/features/protect.ts';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let book = $state<HandbookView | null>(null);
	let error = $state('');
	let busy = $state(false);
	let opened = $state('');
	let off = $state(false);

	async function load() {
		try {
			book = await loadHandbook(i18n.locale);
			off = false;
		} catch (e) {
			if (e instanceof ApiError && e.code === 'feature_off') off = true;
			else error = apiErrorText(e);
		}
	}
	$effect(() => {
		void i18n.locale;
		void load();
	});
	async function refresh() {
		busy = true;
		error = '';
		try {
			await refreshHandbook();
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function pdf(format: 'pdf' | 'md') {
		error = '';
		try {
			await downloadHandbook(i18n.locale, format);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	function made(at: string) {
		const d = new Date(at);
		const day =
			d.toDateString() === new Date().toDateString() ? t('agent.f.protect.today') : formatDate(d);
		return `${day} ${formatTime(d)}`;
	}
	const names = (list: NamedFact[] | null | undefined) =>
		(list ?? []).map((x) => x.name).join(', ');
	const remote = (list: NamedFact[] | null | undefined) =>
		(list ?? []).map((x) => (x.detail ? `${x.name} (${x.detail})` : x.name)).join(', ');
	const apps = $derived(
		book
			? [...(book.facts.apps ?? []), ...(book.facts.vms ?? [])].map((x) => x.name).join(', ')
			: '',
	);
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if off}
	<p class="muted">{t('agent.f.notThere')}</p>
{:else if book}
	<div class="pgrp">
		<div class="prow">
			<span class="pic" style:--c="#6366f1" aria-hidden="true">📕</span>
			<span class="pt"
				><strong>{t('agent.f.protect.handbook')}</strong><small
					>{t('agent.f.protect.h.made', { when: made(book.made) })}</small
				></span
			>
			<button type="button" class="btn" onclick={() => pdf('pdf')}>PDF</button>
		</div>
		{#if admin}
			<div class="prow wrap">
				<span class="pt" style:flex-basis="100%"
					><small>{t('agent.f.protect.h.nightly')}</small></span
				>
				<button type="button" class="btn" disabled={busy} onclick={refresh}
					>{t('agent.f.protect.h.refresh')}</button
				>
				<button type="button" class="btn" onclick={() => pdf('md')}>Markdown</button>
			</div>
		{/if}
	</div>

	<p class="pcap">{t('agent.f.protect.h.build')}</p>
	<div class="pgrp">
		{#each book.volumes ?? [] as v (v.name)}
			<div class="prow">
				<span class="pt"
					><strong>{t('agent.f.protect.h.volume', { name: v.name })}</strong><small>{v.line}</small
					></span
				>
			</div>
		{:else}
			<div class="prow">
				<span class="pt"><small>{t('agent.f.protect.h.noVolumes')}</small></span>
			</div>
		{/each}
		<div class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.h.shares')}</strong><small
					>{names(book.facts.shares) || t('agent.f.protect.h.none')}</small
				></span
			>
		</div>
		<div class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.h.apps')}</strong><small
					>{apps || t('agent.f.protect.h.none')}</small
				></span
			>
		</div>
		{#if book.facts.network?.length}
			<div class="prow">
				<span class="pt"
					><strong>{t('agent.f.protect.h.network')}</strong><small
						>{remote(book.facts.network)}</small
					></span
				>
			</div>
		{/if}
		<div class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.h.remote')}</strong><small
					>{remote(book.facts.remote) || t('agent.f.protect.h.noRemote')}</small
				></span
			>
		</div>
		<div class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.h.backups')}</strong><small
					>{remote(book.facts.backups) || t('agent.f.protect.h.noBackups')}</small
				></span
			>
		</div>
		{#if book.facts.probes?.length}
			<div class="prow">
				<span class="pt"
					><strong>{t('agent.f.protect.h.probes')}</strong><small>{remote(book.facts.probes)}</small
					></span
				>
			</div>
		{/if}
		{#if book.facts.contacts?.length}
			<div class="prow">
				<span class="pt"
					><strong>{t('agent.f.protect.h.contacts')}</strong><small
						>{book.facts.contacts
							.map((c) => (c.phone ? `${c.name} (${c.phone})` : c.name))
							.join(', ')}</small
					></span
				>
			</div>
		{/if}
	</div>

	<p class="pcap">{t('agent.f.protect.h.whatIf')}</p>
	<div class="pgrp">
		{#each book.sections as s (s.id)}
			<button
				type="button"
				class="prow sec"
				aria-expanded={opened === s.id}
				onclick={() => (opened = opened === s.id ? '' : s.id)}
			>
				<span
					class="pic"
					style:--c={{ disk: '#ef4444', boot: '#f97316', stick: '#eab308', data: '#3b82f6' }[
						s.id
					] ?? '#64748b'}
					aria-hidden="true"
					>{{ disk: '💽', boot: '🔌', stick: '🔑', data: '🗂️' }[s.id] ?? '📄'}</span
				>
				<span class="pt"
					><strong>{s.title}</strong><small
						>{t('agent.f.protect.h.steps', { count: s.steps.length })} · {s.sub}</small
					></span
				>
				<span class="chev" aria-hidden="true">{opened === s.id ? '⌄' : '›'}</span>
			</button>
			{#if opened === s.id}
				<ol class="steps">
					{#each s.steps as step, i (i)}<li>{step}</li>{/each}
				</ol>
			{/if}
		{/each}
	</div>
{:else if !error}
	<p class="muted">{t('agent.f.protect.loading')}</p>
{/if}

<style>
	.sec {
		cursor: pointer;
	}
	.sec:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.chev {
		color: var(--muted);
		width: 14px;
		text-align: center;
	}
	.steps {
		margin: 0;
		padding: var(--space-1) var(--space-4) var(--space-3) 36px;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		line-height: 1.45;
		overflow-wrap: anywhere;
	}
</style>
