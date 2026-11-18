<script lang="ts">
	// 1.13 (owner 18 Nov 2026): two possible duplicates side by side and
	// merged into one – field by field: where both have a different name,
	// company, birthday, note or picture, one is chosen (the note also
	// both); numbers, e-mail and postal addresses are joined and each can
	// be left out. The kept contact keeps its place on the iPhone; what
	// TitanOS does not show of either card stays (titand), the other
	// contact's groups name the kept one, and the other is deleted after a
	// question (Dialog). Sets of more than two are merged two at a time.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { ApiError } from '#lib/api.ts';
	import { addressLines, type Contact, type DuplicateSet } from '../contacts.ts';
	import { loadContact, mergeContacts, type Merged } from '../contacts-api.ts';
	import { birthdayLine, typeLabel } from '../contacts-text.ts';
	import { defaultPick, mergeInput, planOf, type MergePick } from '../merge.ts';
	import Avatar from './Avatar.svelte';

	type Props = {
		set: DuplicateSet;
		/** The name of a contact of the set (from the list). */
		nameOf: (id: string) => string;
		today: string;
		phone: boolean;
		onmerged: (keep: string, drop: string, saved: Merged) => void;
		onhide: () => void;
	};
	let { set, nameOf, today, phone, onmerged, onhide }: Props = $props();

	let aId = $state('');
	let bId = $state('');
	$effect(() => {
		// A new set starts with its first two.
		const ids = set.ids;
		if (!ids.includes(aId) || !ids.includes(bId) || aId === bId) {
			aId = ids[0];
			bId = ids[1];
		}
	});

	let a = $state<Contact | null>(null);
	let b = $state<Contact | null>(null);
	let error = $state('');
	let asked = 0;
	async function load(ia: string, ib: string) {
		const mine = ++asked;
		error = '';
		try {
			const [ca, cb] = await Promise.all([loadContact(ia), loadContact(ib)]);
			if (mine !== asked) return;
			a = ca;
			b = cb;
			pick = defaultPick();
		} catch (err) {
			if (mine === asked) error = apiErrorText(err);
		}
	}
	$effect(() => {
		if (aId && bId && aId !== bId) void load(aId, bId);
	});

	let pick = $state<MergePick>(defaultPick());
	const plan = $derived(a && b ? planOf(a, b) : null);
	const nameText = (n: { first: string; last: string }) =>
		[n.first, n.last].filter((x) => x.trim()).join(' ');

	function swap() {
		[aId, bId] = [bId, aId];
	}
	function toggleSkip(list: 'skipPhones' | 'skipEmails' | 'skipAddresses', i: number, on: boolean) {
		pick[list] = on ? pick[list].filter((x) => x !== i) : [...pick[list], i];
	}

	let asking = $state(false);
	let busy = $state(false);
	async function run() {
		if (!a || !b || !plan) return;
		busy = true;
		error = '';
		try {
			const { input, photo } = mergeInput(plan, pick);
			const saved = await mergeContacts(
				{ id: a.id, etag: a.etag },
				{ id: b.id, etag: b.etag },
				input,
				photo,
			);
			asking = false;
			onmerged(a.id, b.id, saved);
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'contacts_conflict'
					? t('contacts.merge.conflict')
					: apiErrorText(err);
			asking = false;
			if (err instanceof ApiError && err.code === 'contacts_conflict') void load(aId, bId);
		} finally {
			busy = false;
		}
	}
	const dropName = $derived(b ? b.name || t('contacts.unnamed') : '');
	const keepName = $derived(a ? a.name || t('contacts.unnamed') : '');
</script>

{#snippet radio(
	group: 'name' | 'org' | 'birthday' | 'notes' | 'photo',
	side: 'a' | 'b' | 'both',
	label: string,
)}
	<label class="opt"
		><input
			type="radio"
			name="merge-{group}"
			checked={pick[group] === side}
			onchange={() => ((pick as Record<string, unknown>)[group] = side)}
		/><span>{label}</span></label
	>
{/snippet}

<article class="merge" class:phone data-merge>
	<h2>{t('contacts.merge.title')}</h2>
	<p class="muted lead">
		{t('contacts.merge.why')}
		{#each set.matches as m, i (i)}{i ? ' · ' : ' '}<span class="why"
				>{t(`contacts.dup.by.${m.kind}` as MessageKey, { value: m.value })}</span
			>{/each}
	</p>
	{#if set.ids.length > 2}
		<div class="pair">
			<label
				><span>{t('contacts.merge.keep')}</span><select class="field" bind:value={aId}
					>{#each set.ids as id (id)}<option value={id} disabled={id === bId}>{nameOf(id)}</option
						>{/each}</select
				></label
			>
			<label
				><span>{t('contacts.merge.into')}</span><select class="field" bind:value={bId}
					>{#each set.ids as id (id)}<option value={id} disabled={id === aId}>{nameOf(id)}</option
						>{/each}</select
				></label
			>
		</div>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if !a || !b || !plan || a.id !== aId || b.id !== bId}
		{#if !error}<p class="muted" role="status">{t('common.loading')}</p>{/if}
	{:else}
		<div class="heads">
			<div class="who">
				<Avatar
					id={a.id}
					name={a.name}
					org={a.org}
					hasPhoto={a.hasPhoto}
					version={a.etag}
					size={44}
				/>
				<span><b>{keepName}</b><small class="muted">{t('contacts.merge.stays')}</small></span>
			</div>
			<button type="button" class="btn swap" onclick={swap} data-merge-swap
				><Icon name="repeat" size={15} />{t('contacts.merge.swap')}</button
			>
			<div class="who">
				<Avatar
					id={b.id}
					name={b.name}
					org={b.org}
					hasPhoto={b.hasPhoto}
					version={b.etag}
					size={44}
				/>
				<span><b>{dropName}</b><small class="muted">{t('contacts.merge.goes')}</small></span>
			</div>
		</div>

		<div class="fields">
			{#if plan.name.differ}
				<fieldset>
					<legend>{t('contacts.merge.name')}</legend>
					{@render radio('name', 'a', nameText(plan.name.a))}
					{@render radio('name', 'b', nameText(plan.name.b))}
				</fieldset>
			{/if}
			{#if plan.org.differ}
				<fieldset>
					<legend>{t('contacts.form.org')}</legend>
					{@render radio('org', 'a', plan.org.a)}
					{@render radio('org', 'b', plan.org.b)}
				</fieldset>
			{/if}
			{#if plan.birthday.differ}
				<fieldset>
					<legend>{t('contacts.field.birthday')}</legend>
					{@render radio('birthday', 'a', birthdayLine(plan.birthday.a, null, today))}
					{@render radio('birthday', 'b', birthdayLine(plan.birthday.b, null, today))}
				</fieldset>
			{/if}
			{#if plan.photo.a && plan.photo.b}
				<fieldset>
					<legend>{t('contacts.merge.photo')}</legend>
					{@render radio('photo', 'a', t('contacts.merge.photoOf', { name: keepName }))}
					{@render radio('photo', 'b', t('contacts.merge.photoOf', { name: dropName }))}
				</fieldset>
			{/if}
			{#if plan.phones.length}
				<fieldset>
					<legend>{t('contacts.field.phone')}</legend>
					{#each plan.phones as p, i (i)}
						<label class="opt"
							><input
								type="checkbox"
								checked={!pick.skipPhones.includes(i)}
								onchange={(e) => toggleSkip('skipPhones', i, e.currentTarget.checked)}
							/><span
								>{p.value.value}
								<small class="muted">{typeLabel(p.value.type, p.value.label)}</small></span
							></label
						>
					{/each}
				</fieldset>
			{/if}
			{#if plan.emails.length}
				<fieldset>
					<legend>{t('contacts.field.email')}</legend>
					{#each plan.emails as e, i (i)}
						<label class="opt"
							><input
								type="checkbox"
								checked={!pick.skipEmails.includes(i)}
								onchange={(ev) => toggleSkip('skipEmails', i, ev.currentTarget.checked)}
							/><span>{e.value.value}</span></label
						>
					{/each}
				</fieldset>
			{/if}
			{#if plan.addresses.length}
				<fieldset>
					<legend>{t('contacts.field.address')}</legend>
					{#each plan.addresses as ad, i (i)}
						<label class="opt"
							><input
								type="checkbox"
								checked={!pick.skipAddresses.includes(i)}
								onchange={(e) => toggleSkip('skipAddresses', i, e.currentTarget.checked)}
							/><span class="lines">{addressLines(ad.value).join(', ')}</span></label
						>
					{/each}
				</fieldset>
			{/if}
			{#if plan.notes.differ}
				<fieldset>
					<legend>{t('contacts.field.notes')}</legend>
					{@render radio('notes', 'a', plan.notes.a)}
					{@render radio('notes', 'b', plan.notes.b)}
					{@render radio('notes', 'both', t('contacts.merge.notesBoth'))}
				</fieldset>
			{/if}
		</div>
		<p class="muted small">{t('contacts.merge.rest')}</p>
		<div class="acts">
			<button type="button" class="btn" onclick={onhide} data-merge-hide
				>{t('contacts.merge.hide')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy}
				onclick={() => (asking = true)}
				data-merge-run><Icon name="check" size={15} />{t('contacts.merge.run')}</button
			>
		</div>
	{/if}
</article>

{#if asking && a && b}
	<Dialog title={t('contacts.merge.askTitle')} locked={busy} onclose={() => (asking = false)}>
		<p class="lead">{t('contacts.merge.askText', { keep: keepName, drop: dropName })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={run} data-merge-confirm
				>{t('contacts.merge.confirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.merge {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		max-width: 46rem;
	}
	h2 {
		margin: 0;
		font-size: var(--text-xl);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.lead,
	.small {
		margin: 0;
		overflow-wrap: break-word;
	}
	.small {
		font-size: var(--text-sm);
	}
	.why {
		overflow-wrap: anywhere;
	}
	.pair {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	.pair label {
		display: flex;
		flex-direction: column;
		gap: 4px;
		flex: 1 1 14rem;
		min-width: 0;
	}
	.pair select {
		max-width: 100%;
	}
	.heads {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
	}
	.who {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1 1 12rem;
		min-width: 0;
	}
	.who span {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.swap {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.fields {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin: 0;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	legend {
		padding: 0 var(--space-1);
		color: var(--muted);
	}
	.opt {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		min-height: 40px;
		padding: var(--space-1) 0;
	}
	.opt input {
		flex: none;
		margin-top: 3px;
	}
	.opt span {
		min-width: 0;
		white-space: pre-line;
		overflow-wrap: anywhere;
	}
	.phone .opt {
		min-height: 44px;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	.acts .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.phone .acts .btn {
		min-height: 44px;
	}
</style>
