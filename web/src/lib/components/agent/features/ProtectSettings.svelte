<script lang="ts">
	import { untrack } from 'svelte';
	// Settings of the functions of "Schutz" (PLAN 54-E: every way of a
	// question is a setting; the owner's answer is the value ab Werk). One
	// column, numbers as fields, no long sliders, no wizard.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Features } from '#lib/agent-features.ts';
	import type {
		Contact,
		Emergency,
		Guard,
		GuardMode,
		Handbook,
		Person,
	} from '#lib/features/protect.ts';

	type Props = {
		id: 'guard' | 'handbook' | 'emergency';
		nas: Features;
		savenas: (next: Features) => Promise<void>;
	};
	let { id, nas, savenas }: Props = $props();

	const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v ?? {})) as T;
	let guard = $state<Guard>(untrack(() => clone(nas.guard)));
	let handbook = $state<Handbook>(untrack(() => clone(nas.handbook)));
	let emergency = $state<Emergency>(untrack(() => clone(nas.emergency)));
	let shares = $state<string[]>([]);
	let accounts = $state<string[]>([]);
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);

	$effect(() => {
		api
			.get<{ name: string }[]>('/shares')
			.then((l) => (shares = l.map((s) => s.name).sort()))
			.catch(() => (shares = []));
		if (id === 'emergency')
			api
				.get<{ name: string; disabled?: boolean }[]>('/users')
				.then(
					(l) =>
						(accounts = l
							.filter((u) => !u.disabled)
							.map((u) => u.name)
							.sort()),
				)
				.catch(() => (accounts = []));
	});

	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			const next = { ...nas };
			if (id === 'guard') next.guard = { ...guard, enabled: nas.guard.enabled };
			if (id === 'handbook') next.handbook = { ...handbook, enabled: nas.handbook.enabled };
			if (id === 'emergency') next.emergency = { ...emergency, enabled: nas.emergency.enabled };
			await savenas(next);
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	const modes: GuardMode[] = ['report', 'lock', 'auto'];
	const numbers = [
		['known', 'agent.f.protect.s.known'],
		['renames', 'agent.f.protect.s.renames'],
		['rewrites', 'agent.f.protect.s.rewrites'],
		['notes', 'agent.f.protect.s.notes'],
		['windowSec', 'agent.f.protect.s.window'],
		['quietSec', 'agent.f.protect.s.quiet'],
	] as const;
	function toggleIn(list: string[] | undefined, name: string, on: boolean): string[] {
		const l = (list ?? []).filter((x) => x !== name);
		return on ? [...l, name] : l;
	}
	function addPerson() {
		const free = accounts.find((a) => !(emergency.persons ?? []).some((p) => p.account === a));
		if (!free) return;
		emergency.persons = [
			...(emergency.persons ?? []),
			{ account: free, waitDays: 3, shares: [] } as Person,
		];
	}
	function addContact() {
		handbook.contacts = [...(handbook.contacts ?? []), { name: '' } as Contact];
	}
</script>

<p class="pcap">{t('agent.f.protect.s.title')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if id === 'guard'}
	<div class="pgrp">
		<div class="prow wrap" role="radiogroup" aria-label={t('agent.f.protect.s.mode')}>
			<span class="pt"><strong>{t('agent.f.protect.s.mode')}</strong></span>
			{#each modes as m (m)}
				<label class="choice">
					<input type="radio" name="guard-mode" value={m} bind:group={guard.mode} />
					<span class="pt"
						><strong>{t(`agent.f.protect.g.mode.${m}` as MessageKey)}</strong><small
							>{t(`agent.f.protect.s.modeHint.${m}` as MessageKey)}</small
						></span
					>
				</label>
			{/each}
		</div>
		{#each numbers as [key, label] (key)}
			<label class="prow">
				<span class="pt"><strong>{t(label)}</strong></span>
				<input
					class="field num"
					type="number"
					min="1"
					max={key.endsWith('Sec') ? 3600 : 100000}
					bind:value={guard[key]}
				/>
			</label>
		{/each}
		<label class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.s.pre')}</strong><small>{t('agent.f.protect.s.preHint')}</small
				></span
			>
			<input
				type="checkbox"
				checked={!guard.noPreSnapshots}
				onchange={(e) => (guard.noPreSnapshots = !e.currentTarget.checked)}
			/>
		</label>
		<div class="prow wrap">
			<span class="pt"
				><strong>{t('agent.f.protect.s.exclude')}</strong><small
					>{t('agent.f.protect.s.excludeHint')}</small
				></span
			>
			<div class="checks">
				{#each shares as name (name)}
					<label
						><input
							type="checkbox"
							checked={guard.exclude?.includes(name)}
							onchange={(e) =>
								(guard.exclude = toggleIn(guard.exclude, name, e.currentTarget.checked))}
						/>{name}</label
					>
				{/each}
			</div>
		</div>
	</div>
{:else if id === 'handbook'}
	<div class="pgrp">
		<label class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.s.probes')}</strong><small
					>{t('agent.f.protect.s.probesHint')}</small
				></span
			>
			<input type="checkbox" bind:checked={handbook.probes} />
		</label>
		<label class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.s.contacts')}</strong><small
					>{t('agent.f.protect.s.contactsHint')}</small
				></span
			>
			<input type="checkbox" bind:checked={handbook.withContacts} />
		</label>
		{#if handbook.withContacts}
			{#each handbook.contacts ?? [] as c, i (i)}
				<div class="prow wrap">
					<input
						class="field grow"
						placeholder={t('agent.f.protect.s.cName')}
						aria-label={t('agent.f.protect.s.cName')}
						maxlength="80"
						bind:value={c.name}
					/>
					<input
						class="field grow"
						placeholder={t('agent.f.protect.s.cPhone')}
						aria-label={t('agent.f.protect.s.cPhone')}
						maxlength="40"
						bind:value={c.phone}
					/>
					<input
						class="field full"
						placeholder={t('agent.f.protect.s.cNote')}
						aria-label={t('agent.f.protect.s.cNote')}
						maxlength="200"
						bind:value={c.note}
					/>
					<button
						type="button"
						class="btn"
						onclick={() =>
							(handbook.contacts = (handbook.contacts ?? []).filter((_, j) => j !== i))}
						>{t('agent.f.protect.s.remove')}</button
					>
				</div>
			{/each}
			<div class="prow">
				<button
					type="button"
					class="btn"
					disabled={(handbook.contacts?.length ?? 0) >= 10}
					onclick={addContact}>{t('agent.f.protect.s.addContact')}</button
				>
			</div>
		{/if}
	</div>
{:else}
	<div class="pgrp">
		{#each emergency.persons ?? [] as p, i (i)}
			<div class="prow wrap person">
				<label class="inline"
					><span>{t('agent.f.protect.s.account')}</span>
					<select class="field" bind:value={p.account}>
						{#each accounts as a (a)}<option value={a}>{a}</option>{/each}
					</select></label
				>
				<label class="inline"
					><span>{t('agent.f.protect.s.wait')}</span>
					<input class="field num" type="number" min="1" max="60" bind:value={p.waitDays} /></label
				>
				<div class="checks full" role="group" aria-label={t('agent.f.protect.s.shares')}>
					<span class="muted">{t('agent.f.protect.s.shares')}</span>
					{#each shares as name (name)}
						<label
							><input
								type="checkbox"
								checked={p.shares.includes(name)}
								onchange={(e) => (p.shares = toggleIn(p.shares, name, e.currentTarget.checked))}
							/>{name}</label
						>
					{/each}
				</div>
				<button
					type="button"
					class="btn"
					onclick={() => (emergency.persons = (emergency.persons ?? []).filter((_, j) => j !== i))}
					>{t('agent.f.protect.s.remove')}</button
				>
			</div>
		{/each}
		<div class="prow">
			<button type="button" class="btn" disabled={!accounts.length} onclick={addPerson}
				>{t('agent.f.protect.s.addPerson')}</button
			>
		</div>
		<label class="prow">
			<span class="pt"
				><strong>{t('agent.f.protect.s.code')}</strong><small
					>{t('agent.f.protect.s.codeHint')}</small
				></span
			>
			<input type="checkbox" bind:checked={emergency.code} />
		</label>
	</div>
	<p class="muted small">{t('agent.f.protect.s.readOnly')}</p>
{/if}

<div class="save">
	<button type="button" class="btn btn-primary" disabled={busy} onclick={save}
		>{t('common.save')}</button
	>
	{#if saved}<span role="status">{t('agent.f.saved')}</span>{/if}
</div>

<style>
	.choice {
		flex-basis: 100%;
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
		padding: var(--space-1) 0;
		cursor: pointer;
	}
	.choice input {
		margin-top: var(--space-1);
	}
	.num {
		width: 96px;
		flex-shrink: 0;
	}
	.checks {
		flex-basis: 100%;
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5) 14px;
	}
	.checks label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		overflow-wrap: anywhere;
	}
	.grow {
		flex: 1 1 140px;
		min-width: 0;
	}
	.full {
		flex-basis: 100%;
	}
	.inline {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		flex: 1 1 140px;
		min-width: 0;
	}
	.inline span {
		font-size: var(--text-sm, 0.875rem);
		color: var(--muted);
	}
	.person {
		align-items: flex-end;
	}
	.save {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}
	.small {
		font-size: var(--text-sm, 0.875rem);
		margin: 0 2px;
	}
</style>
