<script lang="ts">
	// 1.11 (step 16): a contact new or changed (drafts 12, 52): picture
	// (choose a file, from the files of the NAS with the account's rights,
	// remove – applied when saving), first and last name, company, several
	// numbers, e-mail addresses and addresses each with its kind, birthday
	// also without year, note, and for a saved contact the danger zone
	// "Kontakt löschen …" (its own dialog; Enter never deletes). The page
	// saves (the head's "Speichern" or Enter in a field) and sends only what
	// changed.
	// 1.13 (owner 18 Nov 2026): a new picture is cut square first
	// (PhotoCrop); the groups of the contact as boxes to tick.
	// 1.15 (PLAN 115-6, draft 44): Apple's "Neuer Kontakt" – the round
	// picture with "Foto hinzufügen" (a menu: a file, the NAS, remove),
	// first and last name and company in one group, then a group per kind
	// with its rows (a red "−" takes one out) and a green "+ … hinzufügen";
	// birthday and groups show their fields once added or filled. Deleting
	// is the red row at the end (its own dialog). The head with Abbrechen
	// and Fertig is the page's.
	import { tick } from 'svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import NasPicker from '#lib/components/agent/NasPicker.svelte';
	import { thumbnailURL } from '#lib/file-actions.ts';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { nasFileOf } from '#lib/tmail/api.ts';
	import {
		ADDRESS_TYPES,
		MAIL_TYPES,
		PHONE_TYPES,
		pictureName,
		type Contact,
		type Form,
		type Group,
		type PhotoChoice,
	} from '../contacts.ts';
	import { typeLabel } from '../contacts-text.ts';
	import Avatar from './Avatar.svelte';
	import PhotoCrop from './PhotoCrop.svelte';

	type Props = {
		form: Form;
		photo: PhotoChoice;
		/** The saved contact; null for a new one. */
		contact: Contact | null;
		phone: boolean;
		busy: boolean;
		/** The groups of the address book; `member` the ids of those the contact is in. */
		groups?: Group[];
		member?: string[];
		onsubmit: () => void;
		ondelete: () => void;
	};
	let {
		form = $bindable(),
		photo = $bindable(),
		contact,
		phone,
		busy,
		groups = [],
		member = $bindable([]),
		onsubmit,
		ondelete,
	}: Props = $props();

	/** The picture being cut: its address and what to send when it cannot be cut. */
	let cropping = $state<{ src: string; raw: PhotoChoice; own: boolean } | null>(null);
	function endCrop() {
		if (cropping?.own) URL.revokeObjectURL(cropping.src);
		cropping = null;
	}
	function toggleGroup(id: string, on: boolean) {
		member = on ? [...member.filter((g) => g !== id), id] : member.filter((g) => g !== id);
	}

	const uid = $props.id();
	let fileInput: HTMLInputElement | undefined = $state();
	let picking = $state(false);
	let root: HTMLFormElement | undefined = $state();

	const name = $derived(
		[form.first, form.last]
			.map((s) => s.trim())
			.filter(Boolean)
			.join(' ') ||
			form.org.trim() ||
			contact?.name ||
			'',
	);
	const preview = $derived(photo.kind === 'file' || photo.kind === 'nas' ? photo.preview : '');
	const hasPhoto = $derived(photo.kind === 'remove' ? false : !!contact?.hasPhoto);

	function dropPreview() {
		if (photo.kind === 'file') URL.revokeObjectURL(photo.preview);
	}
	function chooseFile(e: Event & { currentTarget: HTMLInputElement }) {
		const file = e.currentTarget.files?.[0];
		e.currentTarget.value = '';
		if (!file) return;
		const src = URL.createObjectURL(file);
		cropping = { src, own: true, raw: { kind: 'file', file, preview: src } };
	}
	function cropped(file: File, preview: string) {
		dropPreview();
		photo = { kind: 'file', file, preview };
		endCrop();
	}
	function takeRaw() {
		if (!cropping) return;
		dropPreview();
		photo = cropping.raw;
		// The object URL is the preview now: not revoked with the crop.
		cropping = null;
	}
	function chooseNas(refs: string[]) {
		picking = false;
		const ref = refs[0];
		if (!ref) return;
		const { root, path } = nasFileOf(ref);
		// Cut from the NAS's large preview (1280 px); as it is when it cannot be read.
		cropping = {
			src: thumbnailURL(root, path, 1280),
			own: false,
			raw: { kind: 'nas', root, path, preview: thumbnailURL(root, path, 176) },
		};
	}
	function removePicture() {
		dropPreview();
		photo = contact?.hasPhoto ? { kind: 'remove' } : { kind: 'keep' };
	}
	$effect(() => () => dropPreview());

	async function add(list: 'phones' | 'emails' | 'addresses') {
		if (list === 'addresses')
			form.addresses.push({
				type: 'home',
				street: '',
				postcode: '',
				city: '',
				region: '',
				country: '',
			});
		else form[list].push({ type: list === 'phones' ? 'mobile' : 'home', value: '' });
		await tick();
		root?.querySelector<HTMLElement>(`[data-list="${list}"] .row:last-of-type .first`)?.focus();
	}

	const typeName = (type: string) => t(`contacts.type.${type}` as MessageKey);
	const months = $derived(
		Array.from({ length: 12 }, (_, i) =>
			new Intl.DateTimeFormat(i18n.locale === 'en' ? 'en-GB' : 'de-DE', { month: 'long' }).format(
				new Date(2000, i, 15),
			),
		),
	);
	const pad = (n: number) => String(n).padStart(2, '0');
	const bMonth = $derived(Number(form.birthdayDate.slice(5, 7)) || 0);
	const bDay = $derived(Number(form.birthdayDate.slice(8, 10)) || 0);
	function setNoYear(on: boolean) {
		form.birthdayNoYear = on;
		if (on && form.birthdayDate) form.birthdayDate = '2000' + form.birthdayDate.slice(4);
	}
	function setDay(day: number, month: number) {
		form.birthdayDate = day && month ? `2000-${pad(month)}-${pad(day)}` : '';
	}
	// Birthday and groups show their fields once filled or added.
	let wantBirthday = $state(false);
	let wantGroups = $state(false);
	// What the saved card has stays shown while it is being emptied.
	const showBirthday = $derived(wantBirthday || !!form.birthdayDate || !!contact?.birthday);
	const showGroups = $derived(
		wantGroups ||
			member.length > 0 ||
			(!!contact && groups.some((g) => g.members.includes(contact.id))),
	);
	async function reveal(what: 'birthday' | 'groups') {
		if (what === 'birthday') wantBirthday = true;
		else wantGroups = true;
		await tick();
		root?.querySelector<HTMLElement>(`[data-reveal="${what}"] :is(input, select)`)?.focus();
	}
</script>

{#snippet typeSelect(row: { type: string; label?: string }, types: readonly string[], what: string)}
	<select class="lab first" aria-label={t('contacts.form.typeOf', { what })} bind:value={row.type}>
		{#each types as type (type)}
			<option value={type}
				>{type === 'other' && row.label ? typeLabel(type, row.label) : typeName(type)}</option
			>
		{/each}
	</select>
{/snippet}

{#snippet addRow(text: string, onclick: () => void, data: string)}
	<button type="button" class="addrow" {onclick} data-add={data}
		><span class="addp" aria-hidden="true"><Icon name="plus" size={14} /></span><span class="at"
			>{text}</span
		></button
	>
{/snippet}

{#snippet removeButton(what: string, onclick: () => void)}
	<button
		type="button"
		class="rm"
		aria-label={t('contacts.form.remove', { what })}
		title={t('contacts.form.remove', { what })}
		{onclick}><span class="minus" aria-hidden="true"></span></button
	>
{/snippet}

<form
	class="edit"
	class:phone
	bind:this={root}
	data-contact-editor
	onsubmit={(e) => {
		e.preventDefault();
		if (!busy) onsubmit();
	}}
>
	<div class="photo">
		{#if preview || hasPhoto}
			<Avatar
				id={contact?.id ?? ''}
				{name}
				{hasPhoto}
				{preview}
				version={contact?.etag}
				size={96}
			/>
		{:else}
			<span class="cam" aria-hidden="true"><Icon name="camera" size={34} /></span>
		{/if}
		<Dropdown
			label={preview || hasPhoto ? t('contacts.form.photoChange') : t('contacts.form.photoAdd')}
			text={preview || hasPhoto ? t('contacts.form.photoChange') : t('contacts.form.photoAdd')}
		>
			{#snippet children(close)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						fileInput?.click();
					}}>{t('contacts.form.photoPick')}</button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						picking = true;
					}}>{t('contacts.form.photoNas')}</button
				>
				{#if preview || hasPhoto}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							removePicture();
						}}>{t('contacts.form.photoRemove')}</button
					>
				{/if}
			{/snippet}
		</Dropdown>
		<small class="muted" role="status"
			>{photo.kind === 'file' || photo.kind === 'nas'
				? t('contacts.form.photoNew')
				: photo.kind === 'remove'
					? t('contacts.form.photoGone')
					: t('contacts.form.photoHint')}</small
		>
		<input
			bind:this={fileInput}
			class="sr-only"
			type="file"
			accept="image/jpeg,image/png,image/webp"
			tabindex="-1"
			aria-hidden="true"
			onchange={chooseFile}
		/>
	</div>

	<div class="grp names">
		<input
			class="cell"
			autocomplete="off"
			aria-label={t('contacts.form.first')}
			placeholder={t('contacts.form.first')}
			bind:value={form.first}
		/>
		<input
			class="cell"
			autocomplete="off"
			aria-label={t('contacts.form.last')}
			placeholder={t('contacts.form.last')}
			bind:value={form.last}
		/>
		<input
			class="cell"
			autocomplete="off"
			aria-label={t('contacts.form.org')}
			placeholder={t('contacts.form.org')}
			bind:value={form.org}
		/>
	</div>

	<fieldset class="grp" data-list="phones">
		<legend class="sr-only">{t('contacts.field.phone')}</legend>
		{#each form.phones as row, i (i)}
			<div class="row">
				{@render removeButton(row.value || t('contacts.field.phone'), () =>
					form.phones.splice(i, 1),
				)}{@render typeSelect(row, PHONE_TYPES, t('contacts.field.phone'))}<input
					class="cell grow"
					type="tel"
					aria-label={t('contacts.form.number')}
					placeholder={t('contacts.form.number')}
					bind:value={row.value}
				/>
			</div>
		{/each}
		{@render addRow(t('contacts.form.addPhone'), () => add('phones'), 'phone')}
	</fieldset>

	<fieldset class="grp" data-list="emails">
		<legend class="sr-only">{t('contacts.field.email')}</legend>
		{#each form.emails as row, i (i)}
			<div class="row">
				{@render removeButton(row.value || t('contacts.field.email'), () =>
					form.emails.splice(i, 1),
				)}{@render typeSelect(row, MAIL_TYPES, t('contacts.field.email'))}<input
					class="cell grow"
					type="email"
					aria-label={t('contacts.form.email')}
					placeholder={t('contacts.form.email')}
					bind:value={row.value}
				/>
			</div>
		{/each}
		{@render addRow(t('contacts.form.addEmail'), () => add('emails'), 'email')}
	</fieldset>

	<fieldset class="grp" data-list="addresses">
		<legend class="sr-only">{t('contacts.field.address')}</legend>
		{#each form.addresses as row, i (i)}
			<div class="row adr">
				{@render removeButton(t('contacts.field.address'), () =>
					form.addresses.splice(i, 1),
				)}{@render typeSelect(row, ADDRESS_TYPES, t('contacts.field.address'))}
				<span class="adr-fields">
					<textarea
						class="cell street"
						rows={Math.max(1, row.street.split('\n').length)}
						aria-label={t('contacts.form.street')}
						placeholder={t('contacts.form.street')}
						bind:value={row.street}></textarea>
					<span class="pair"
						><input
							class="cell plz"
							aria-label={t('contacts.form.postcode')}
							placeholder={t('contacts.form.postcode')}
							bind:value={row.postcode}
						/><input
							class="cell grow"
							aria-label={t('contacts.form.city')}
							placeholder={t('contacts.form.city')}
							bind:value={row.city}
						/></span
					>
					{#if row.region}
						<input
							class="cell"
							aria-label={t('contacts.form.region')}
							placeholder={t('contacts.form.region')}
							bind:value={row.region}
						/>
					{/if}
					<input
						class="cell"
						aria-label={t('contacts.form.country')}
						placeholder={t('contacts.form.country')}
						bind:value={row.country}
					/>
				</span>
			</div>
		{/each}
		{@render addRow(t('contacts.form.addAddress'), () => add('addresses'), 'address')}
	</fieldset>

	{#if showBirthday}
		<fieldset class="grp" data-reveal="birthday">
			<legend class="sr-only">{t('contacts.field.birthday')}</legend>
			<div class="row wrap">
				<span class="lbl">{t('contacts.field.birthday')}</span>
				{#if form.birthdayNoYear}
					<select
						class="field bsel"
						aria-label={t('contacts.form.day')}
						value={bDay}
						onchange={(e) => setDay(Number(e.currentTarget.value), bMonth || 1)}
					>
						<option value={0}>–</option>
						{#each Array.from({ length: 31 }, (_, i) => i + 1) as d (d)}<option value={d}
								>{d}.</option
							>{/each}
					</select>
					<select
						class="field bsel"
						aria-label={t('contacts.form.month')}
						value={bMonth}
						onchange={(e) => setDay(bDay || 1, Number(e.currentTarget.value))}
					>
						<option value={0}>–</option>
						{#each months as m, i (i)}<option value={i + 1}>{m}</option>{/each}
					</select>
				{:else}
					<input
						class="field bsel"
						type="date"
						aria-label={t('contacts.field.birthday')}
						bind:value={form.birthdayDate}
					/>
				{/if}
			</div>
			<div class="row wrap">
				<label class="chk"
					><input
						type="checkbox"
						checked={form.birthdayNoYear}
						onchange={(e) => setNoYear(e.currentTarget.checked)}
					/>
					{t('contacts.form.noYear')}</label
				>
				{#if form.birthdayDate}
					<button
						type="button"
						class="plain"
						onclick={() => {
							form.birthdayDate = '';
							wantBirthday = false;
						}}>{t('contacts.form.noBirthday')}</button
					>
				{/if}
			</div>
			<small class="muted hint">{t('contacts.form.birthdayHint')}</small>
		</fieldset>
	{:else}
		<div class="grp">
			{@render addRow(t('contacts.form.addBirthday'), () => void reveal('birthday'), 'birthday')}
		</div>
	{/if}

	{#if groups.length}
		{#if showGroups}
			<fieldset class="grp" data-editor-groups data-reveal="groups">
				<legend class="sr-only">{t('contacts.groups.title')}</legend>
				{#each groups as g (g.id)}
					<label class="row chk"
						><input
							type="checkbox"
							checked={member.includes(g.id)}
							onchange={(e) => toggleGroup(g.id, e.currentTarget.checked)}
						/>
						{g.name || t('contacts.groups.unnamed')}</label
					>
				{/each}
			</fieldset>
		{:else}
			<div class="grp">
				{@render addRow(t('contacts.form.addGroup'), () => void reveal('groups'), 'group')}
			</div>
		{/if}
	{/if}

	<div class="grp">
		<textarea
			class="cell note"
			rows="3"
			aria-label={t('contacts.field.notes')}
			placeholder={t('contacts.form.notesPlaceholder')}
			bind:value={form.notes}></textarea>
	</div>

	{#if contact}
		<div class="grp">
			<button type="button" class="delrow" onclick={ondelete} data-contact-delete
				>{t('contacts.danger.button')}</button
			>
		</div>
		<p class="muted hint out">{t('contacts.danger.hint')}</p>
	{/if}
	<!-- Enter in a field saves (the head's button is outside the form). -->
	<button type="submit" class="sr-only" tabindex="-1" aria-hidden="true">{t('common.save')}</button>
</form>

{#if cropping}
	<PhotoCrop src={cropping.src} onpick={cropped} onraw={takeRaw} onclose={endCrop} />
{/if}

{#if picking}
	<NasPicker
		max={1}
		only={pictureName}
		title={t('contacts.form.photoNasTitle')}
		onpick={chooseNas}
		onclose={() => (picking = false)}
	/>
{/if}

<style>
	.edit {
		/* The hidden legends and the file field stay inside the column's scroll. */
		position: relative;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		width: 100%;
		max-width: 40rem;
		margin: 0 auto;
	}
	.photo {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		text-align: center;
	}
	.cam {
		display: inline-grid;
		place-items: center;
		width: 96px;
		height: 96px;
		border-radius: 50%;
		background: var(--chip);
		color: var(--text);
	}
	/* "Foto hinzufügen" as a link in the accent colour. */
	.photo :global(.dropdown > .btn) {
		min-height: 44px;
		border: 0;
		background: none;
		color: var(--accent);
		font-weight: 400;
		white-space: normal;
	}
	.photo :global(.dropdown > .btn .chevron) {
		display: none;
	}
	.photo small {
		max-width: 28rem;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.grp {
		position: relative;
		display: flex;
		flex-direction: column;
		min-width: 0;
		margin: 0;
		padding: 0;
		border: 0;
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--surface);
		overflow: clip;
	}
	.grp > :global(*) {
		border-bottom: 1px solid var(--border);
	}
	.grp > :global(*:last-child),
	.grp > :global(legend) {
		border-bottom: 0;
	}
	.cell {
		width: 100%;
		min-width: 0;
		min-height: 48px;
		padding: var(--space-2) var(--space-4);
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 1.0625rem;
	}
	.cell::placeholder {
		color: var(--muted);
	}
	.cell:focus,
	.lab:focus {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.names .cell {
		border-bottom: 1px solid var(--border);
	}
	.names .cell:last-child {
		border-bottom: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 48px;
		padding-left: var(--space-2);
	}
	.row .cell {
		padding-left: var(--space-2);
	}
	.row.wrap {
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-3);
	}
	.row.adr {
		align-items: flex-start;
	}
	.row.adr > .rm,
	.row.adr > .lab {
		margin-top: 2px;
	}
	.adr-fields {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.adr-fields > .cell,
	.pair {
		border-bottom: 1px solid var(--border);
	}
	.adr-fields > .cell:last-child {
		border-bottom: 0;
	}
	.pair {
		display: flex;
		min-width: 0;
	}
	.plz {
		flex: 0 0 6.5rem;
		width: 6.5rem;
	}
	.grow {
		flex: 1 1 8rem;
	}
	.street {
		resize: none;
		field-sizing: content;
		line-height: 1.4;
	}
	.lab {
		flex: none;
		/* As wide as its kind ("Mobil"), so the value keeps the room. */
		field-sizing: content;
		max-width: 8.5rem;
		min-height: 44px;
		padding: 0 var(--space-1);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.lab option {
		color: var(--text);
		background: var(--surface);
	}
	/* The red "−" and the green "+": round, 44 px to tap, the sign also as shape. */
	.rm {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: 0;
		border-radius: 50%;
		background: none;
		cursor: pointer;
	}
	.minus {
		position: relative;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		background: var(--status-error);
	}
	.minus::after {
		content: '';
		position: absolute;
		left: 6px;
		right: 6px;
		top: 10px;
		height: 2px;
		border-radius: 1px;
		background: var(--on-accent);
	}
	.rm:focus-visible,
	.addrow:focus-visible,
	.delrow:focus-visible,
	.plain:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.addrow {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 48px;
		padding: var(--space-2) var(--space-4);
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 1.0625rem;
		text-align: left;
		cursor: pointer;
	}
	.addrow:hover,
	.delrow:hover {
		background: color-mix(in srgb, var(--text) 5%, var(--surface));
	}
	.addp {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		background: var(--status-ok);
		color: var(--on-accent);
	}
	.at {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.lbl {
		color: var(--text);
		font-size: var(--text-md);
	}
	.bsel {
		width: auto;
		flex: none;
		max-width: 100%;
	}
	.chk {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
		overflow-wrap: anywhere;
	}
	label.row.chk {
		display: flex;
		padding: 0 var(--space-4);
		cursor: pointer;
	}
	.plain {
		min-height: 44px;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
	}
	.hint {
		padding: 0 var(--space-4) var(--space-2);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.out {
		margin: calc(-1 * var(--space-2)) 0 0;
		padding: 0 var(--space-4);
		text-align: center;
	}
	.note {
		min-height: 6rem;
		line-height: 1.5;
		resize: vertical;
	}
	.delrow {
		width: 100%;
		min-height: 48px;
		padding: var(--space-2) var(--space-4);
		border: 0;
		background: none;
		color: var(--error-text);
		font: inherit;
		font-size: 1.0625rem;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	small {
		overflow-wrap: break-word;
	}
</style>
