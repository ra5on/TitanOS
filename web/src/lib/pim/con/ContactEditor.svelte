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
	import { tick } from 'svelte';
	import DangerZone from '#lib/components/DangerZone.svelte';
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
</script>

{#snippet typeSelect(row: { type: string; label?: string }, types: readonly string[], what: string)}
	<select
		class="field lab first"
		aria-label={t('contacts.form.typeOf', { what })}
		bind:value={row.type}
	>
		{#each types as type (type)}
			<option value={type}
				>{type === 'other' && row.label ? typeLabel(type, row.label) : typeName(type)}</option
			>
		{/each}
	</select>
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
	<div class="photo-row">
		<Avatar id={contact?.id ?? ''} {name} {hasPhoto} {preview} version={contact?.etag} size={88} />
		<span class="pbtns">
			<button type="button" class="btn" onclick={() => fileInput?.click()}
				><Icon name="image" size={15} />{t('contacts.form.photoPick')}</button
			>
			<button type="button" class="btn" onclick={() => (picking = true)}
				><Icon name="folder" size={15} />{t('contacts.form.photoNas')}</button
			>
			{#if preview || hasPhoto}
				<button type="button" class="btn" onclick={removePicture}
					>{t('contacts.form.photoRemove')}</button
				>
			{/if}
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
		</span>
	</div>

	<div class="grid2">
		<label
			><span class="lbl">{t('contacts.form.first')}</span><input
				class="field"
				autocomplete="off"
				bind:value={form.first}
			/></label
		>
		<label
			><span class="lbl">{t('contacts.form.last')}</span><input
				class="field"
				autocomplete="off"
				bind:value={form.last}
			/></label
		>
		<label class="span2"
			><span class="lbl">{t('contacts.form.org')}</span><input
				class="field"
				autocomplete="off"
				placeholder={t('contacts.form.orgPlaceholder')}
				bind:value={form.org}
			/></label
		>
	</div>

	<fieldset data-list="phones">
		<legend>{t('contacts.field.phone')}</legend>
		{#each form.phones as row, i (i)}
			<span class="multi row"
				>{@render typeSelect(row, PHONE_TYPES, t('contacts.field.phone'))}<input
					class="field grow"
					type="tel"
					aria-label={t('contacts.form.number')}
					bind:value={row.value}
				/><button
					type="button"
					class="ib"
					aria-label={t('contacts.form.remove', { what: row.value || t('contacts.field.phone') })}
					onclick={() => form.phones.splice(i, 1)}><Icon name="close" size={15} /></button
				></span
			>
		{/each}
		<button type="button" class="btn add" onclick={() => add('phones')}
			><Icon name="plus" size={15} />{t('contacts.field.phone')}</button
		>
	</fieldset>

	<fieldset data-list="emails">
		<legend>{t('contacts.field.email')}</legend>
		{#each form.emails as row, i (i)}
			<span class="multi row"
				>{@render typeSelect(row, MAIL_TYPES, t('contacts.field.email'))}<input
					class="field grow"
					type="email"
					aria-label={t('contacts.form.email')}
					bind:value={row.value}
				/><button
					type="button"
					class="ib"
					aria-label={t('contacts.form.remove', { what: row.value || t('contacts.field.email') })}
					onclick={() => form.emails.splice(i, 1)}><Icon name="close" size={15} /></button
				></span
			>
		{/each}
		<button type="button" class="btn add" onclick={() => add('emails')}
			><Icon name="plus" size={15} />{t('contacts.field.email')}</button
		>
	</fieldset>

	<fieldset data-list="addresses">
		<legend>{t('contacts.field.address')}</legend>
		{#each form.addresses as row, i (i)}
			<div class="adr row">
				<span class="multi"
					>{@render typeSelect(row, ADDRESS_TYPES, t('contacts.field.address'))}<textarea
						class="field grow street"
						rows={Math.max(1, row.street.split('\n').length)}
						aria-label={t('contacts.form.street')}
						bind:value={row.street}></textarea><button
						type="button"
						class="ib"
						aria-label={t('contacts.form.remove', { what: t('contacts.field.address') })}
						onclick={() => form.addresses.splice(i, 1)}><Icon name="close" size={15} /></button
					></span
				>
				<span class="multi indent"
					><input
						class="field plz"
						aria-label={t('contacts.form.postcode')}
						placeholder={t('contacts.form.postcode')}
						bind:value={row.postcode}
					/><input
						class="field grow"
						aria-label={t('contacts.form.city')}
						placeholder={t('contacts.form.city')}
						bind:value={row.city}
					/></span
				>
				{#if row.region}
					<span class="multi indent"
						><input
							class="field grow"
							aria-label={t('contacts.form.region')}
							bind:value={row.region}
						/></span
					>
				{/if}
				<span class="multi indent"
					><input
						class="field grow"
						aria-label={t('contacts.form.country')}
						placeholder={t('contacts.form.country')}
						bind:value={row.country}
					/></span
				>
			</div>
		{/each}
		<button type="button" class="btn add" onclick={() => add('addresses')}
			><Icon name="plus" size={15} />{t('contacts.field.address')}</button
		>
	</fieldset>

	<fieldset>
		<legend>{t('contacts.field.birthday')}</legend>
		<span class="multi">
			{#if form.birthdayNoYear}
				<select
					class="field bsel"
					aria-label={t('contacts.form.day')}
					value={bDay}
					onchange={(e) => setDay(Number(e.currentTarget.value), bMonth || 1)}
				>
					<option value={0}>–</option>
					{#each Array.from({ length: 31 }, (_, i) => i + 1) as d (d)}<option value={d}>{d}.</option
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
			<label class="chk"
				><input
					type="checkbox"
					checked={form.birthdayNoYear}
					onchange={(e) => setNoYear(e.currentTarget.checked)}
				/>
				{t('contacts.form.noYear')}</label
			>
			{#if form.birthdayDate}
				<button type="button" class="btn" onclick={() => (form.birthdayDate = '')}
					>{t('contacts.form.noBirthday')}</button
				>
			{/if}
		</span>
		<small class="muted">{t('contacts.form.birthdayHint')}</small>
	</fieldset>

	{#if groups.length}
		<fieldset data-editor-groups>
			<legend>{t('contacts.groups.title')}</legend>
			<span class="multi">
				{#each groups as g (g.id)}
					<label class="chk"
						><input
							type="checkbox"
							checked={member.includes(g.id)}
							onchange={(e) => toggleGroup(g.id, e.currentTarget.checked)}
						/>
						{g.name || t('contacts.groups.unnamed')}</label
					>
				{/each}
			</span>
		</fieldset>
	{/if}

	<fieldset>
		<legend>{t('contacts.field.notes')}</legend>
		<textarea
			class="field note"
			rows="3"
			aria-label={t('contacts.field.notes')}
			bind:value={form.notes}></textarea>
	</fieldset>

	{#if contact}
		<DangerZone hint={t('contacts.danger.hint')}>
			<button type="button" class="btn btn-danger" onclick={ondelete}
				><Icon name="trash" size={15} />{t('contacts.danger.button')}</button
			>
		</DangerZone>
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
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		max-width: 46rem;
	}
	.photo-row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
	}
	.pbtns {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		flex: 1 1 14rem;
		min-width: 0;
	}
	.phone .pbtns {
		flex-direction: column;
		align-items: flex-start;
		min-width: 10rem;
	}
	.pbtns small {
		flex-basis: 100%;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.phone .pbtns small {
		flex-basis: auto;
	}
	.pbtns .btn,
	.add {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.grid2 {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--space-2) var(--space-3);
	}
	.phone .grid2 {
		grid-template-columns: minmax(0, 1fr);
	}
	.grid2 label {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.span2 {
		grid-column: 1 / -1;
	}
	.lbl,
	legend {
		color: var(--muted);
		font-size: var(--text-md);
	}
	fieldset {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	legend {
		padding: 0;
		margin-bottom: var(--space-1);
	}
	.multi {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
	}
	.adr {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		width: 100%;
	}
	.multi.indent {
		padding-left: calc(7rem + var(--space-2));
		padding-right: calc(40px + var(--space-2));
	}
	.phone .multi.indent {
		padding-left: 0;
		padding-right: calc(44px + var(--space-2));
	}
	.lab {
		width: 7rem;
		flex: none;
	}
	.phone .lab {
		width: 6.5rem;
	}
	.grow {
		flex: 1 1 8rem;
		min-width: 0;
	}
	.street {
		resize: none;
		field-sizing: content;
		font: inherit;
		line-height: 1.4;
	}
	.bsel {
		width: auto;
		flex: none;
		max-width: 100%;
	}
	.plz {
		width: 6rem;
		flex: none;
	}
	.chk {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 44px;
		overflow-wrap: anywhere;
	}
	.note {
		width: 100%;
		font: inherit;
		line-height: 1.5;
		resize: vertical;
	}
	.ib {
		flex: none;
		display: inline-grid;
		place-items: center;
		min-width: 40px;
		min-height: 40px;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.ib:hover {
		color: var(--text);
	}
	.ib:focus-visible {
		outline: 2px solid var(--accent);
	}
	.phone .ib {
		min-width: 44px;
		min-height: 44px;
	}
	.phone .field,
	.phone .btn {
		min-height: 44px;
	}
	small {
		overflow-wrap: break-word;
	}
</style>
