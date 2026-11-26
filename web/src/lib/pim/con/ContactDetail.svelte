<script lang="ts">
	// 1.11 (step 16): one contact – picture, name, company, its numbers,
	// e-mail addresses, addresses, birthday, web address and note, each
	// with its kind in words, numbers as tel: links, everything as text.
	// Links only http(s)/mailto/tel/sms, with rel="noopener noreferrer" and
	// their target as the visible text.
	// 1.13: the groups of the contact; each e-mail address opens Mail's
	// compose in a window (only with Mail's release, else a mailto: link).
	// 1.15 (PLAN 115-6, draft 42): like Apple's Kontakte everywhere – on
	// top "‹ Kontakte" (compact) and "Bearbeiten", the big picture or
	// initials, the name, the company, four quick buttons Nachricht (sms:,
	// a mobile number first), Anrufen (tel:), Mail and Teilen (the card as
	// a .vcf: the share sheet of the device, else a download), then the
	// fields in groups on the surface; the address opens the map ("Karte",
	// Apple Maps on Apple devices, else OpenStreetMap – the address leaves
	// the NAS only on a tap). At the end the rows "Termin anlegen", the own
	// card ("Als meine Karte festlegen") and, in an open group, "Aus „…“
	// entfernen". A quick button without a number or address stays in its
	// place, dimmed, its title says why.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import { saveBlob } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { linkPieces, safeHref } from '../calendar.ts';
	import { dateText } from '../calendar-text.ts';
	import {
		addressLines,
		mapHref,
		smsHref,
		telHref,
		vcardName,
		vcardOf,
		webHref,
		type Contact,
		type Group,
		type Soon,
	} from '../contacts.ts';
	import { birthdayLine, typeLabel } from '../contacts-text.ts';
	import Avatar from './Avatar.svelte';

	type Props = {
		contact: Contact;
		soon: Soon | null;
		today: string;
		/** The compact view: "‹ back" on top. */
		compact: boolean;
		/** Mail is released for the account: an address opens its compose. */
		mail: boolean;
		/** The name of the address book ("Kontakte"). */
		book: string;
		/** The groups the contact is in. */
		groups?: Group[];
		/** The label of "‹ …" (compact): the list. */
		backLabel?: string;
		onback?: () => void;
		/** This is the account's own card. */
		isMe?: boolean;
		/** The open group the contact is in ("Aus „…“ entfernen"). */
		inGroup?: string;
		onedit: () => void;
		onmail: (addr: string) => void;
		onevent: () => void;
		onme?: (on: boolean) => void;
		onleavegroup?: () => void;
	};
	let {
		contact,
		soon,
		today,
		compact,
		mail,
		book,
		groups = [],
		backLabel = '',
		onback,
		isMe = false,
		inGroup = '',
		onedit,
		onmail,
		onevent,
		onme,
		onleavegroup,
	}: Props = $props();

	const call = $derived(
		[...contact.phones.filter((p) => p.type === 'mobile'), ...contact.phones]
			.map((p) => telHref(p.value))
			.find(Boolean) ?? '',
	);
	// A message goes to a mobile number first.
	const sms = $derived(
		[...contact.phones.filter((p) => p.type === 'mobile'), ...contact.phones]
			.map((p) => smsHref(p.value))
			.find(Boolean) ?? '',
	);
	const apple =
		typeof navigator !== 'undefined' && /iPhone|iPad|Macintosh/.test(navigator.userAgent);
	const firstMail = $derived(contact.emails.find((e) => e.value.trim())?.value.trim() ?? '');
	const birthday = $derived(birthdayLine(contact.birthday, soon, today));
	const web = $derived(webHref(contact.url));
	const changed = $derived(
		/^\d{4}-\d{2}-\d{2}/.test(contact.modified) ? dateText(contact.modified.slice(0, 10)) : '',
	);
	const title = $derived(contact.name || t('contacts.unnamed'));

	// "Kopiert" as text for a moment; without the clipboard API (no secure
	// context, e.g. http) the old way through a selected field.
	let copied = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;
	async function copy(key: string, text: string) {
		let ok = false;
		try {
			await navigator.clipboard.writeText(text);
			ok = true;
		} catch {
			const area = document.createElement('textarea');
			area.value = text;
			area.setAttribute('readonly', '');
			area.style.position = 'fixed';
			area.style.opacity = '0';
			document.body.append(area);
			area.select();
			try {
				ok = document.execCommand('copy');
			} catch {
				ok = false;
			}
			area.remove();
		}
		copied = ok ? key : '';
		clearTimeout(timer);
		timer = setTimeout(() => (copied = ''), 2000);
	}

	// "Teilen": the device's share sheet with the card as a file; without it
	// (most desktop browsers, no secure context) the card is downloaded.
	let shared = $state('');
	async function share() {
		const name = vcardName(contact);
		const text = vcardOf(contact);
		shared = '';
		try {
			const file = new File([text], name, { type: 'text/vcard' });
			if (navigator.canShare?.({ files: [file] })) {
				await navigator.share({ files: [file], title });
				return;
			}
		} catch (err) {
			// Closed by the person: nothing else happens.
			if (err instanceof DOMException && err.name === 'AbortError') return;
		}
		saveBlob(new Blob([text], { type: 'text/vcard' }), name);
		shared = t('contacts.act.shared', { file: name });
	}
</script>

{#snippet copyButton(key: string, text: string, what: string)}
	<span class="cp">
		{#if copied === key}<small class="done" role="status">{t('contacts.copied')}</small>{/if}
		<button
			type="button"
			class="ib"
			aria-label={t('contacts.copy', { what })}
			title={t('contacts.copy', { what })}
			onclick={() => copy(key, text)}><Icon name="copy" size={16} /></button
		>
	</span>
{/snippet}

{#snippet quick(icon: IconName, label: string, why: string)}
	<span class="qi"><Icon name={icon} size={22} /></span><span class="ql">{label}</span
	>{#if why}<span class="sr-only">{why}</span>{/if}
{/snippet}

<article class="detail" class:compact data-contact-detail={contact.id}>
	{#snippet edit()}
		<button type="button" class="link" onclick={onedit} data-contact-edit
			>{t('contacts.edit')}</button
		>
	{/snippet}
	{#if compact}
		<div class="nav">
			{#if onback}<button type="button" class="link back" onclick={onback} data-back
					><Icon name="chevronLeft" size={22} />{backLabel}</button
				>{:else}<span></span>{/if}
			{@render edit()}
		</div>
	{/if}
	<header class="dh">
		<Avatar
			id={contact.id}
			name={contact.name}
			org={contact.org}
			hasPhoto={contact.hasPhoto}
			version={contact.etag}
			size={compact ? 96 : 64}
		/>
		<div class="identity">
			<h2>{title}</h2>
			{#if contact.org && contact.org !== contact.name}<p class="org">{contact.org}</p>{/if}
		</div>
		{#if !compact}{@render edit()}{/if}
	</header>
	<div class="acts" data-contact-actions>
		{#if sms}<a class="act" href={sms}>{@render quick('chat', t('contacts.act.sms'), '')}</a
			>{:else}<button type="button" class="act off" disabled title={t('contacts.act.noPhone')}
				>{@render quick('chat', t('contacts.act.sms'), t('contacts.act.noPhone'))}</button
			>{/if}
		{#if call}<a class="act" href={call}>{@render quick('phone', t('contacts.act.call'), '')}</a
			>{:else}<button type="button" class="act off" disabled title={t('contacts.act.noPhone')}
				>{@render quick('phone', t('contacts.act.call'), t('contacts.act.noPhone'))}</button
			>{/if}
		{#if firstMail}
			{#if mail}<button type="button" class="act" onclick={() => onmail(firstMail)}
					>{@render quick('mail', t('contacts.act.mailShort'), '')}</button
				>{:else}<a class="act" href="mailto:{firstMail}"
					>{@render quick('mail', t('contacts.act.mailShort'), '')}</a
				>{/if}
		{:else}<button type="button" class="act off" disabled title={t('contacts.act.noMail')}
				>{@render quick('mail', t('contacts.act.mailShort'), t('contacts.act.noMail'))}</button
			>{/if}
		<button
			type="button"
			class="act"
			title={t('contacts.act.shareTitle')}
			onclick={() => void share()}
			data-contact-share>{@render quick('upload', t('contacts.act.share'), '')}</button
		>
	</div>
	{#if shared}<p class="muted shared" role="status">{shared}</p>{/if}

	{#if contact.phones.length}
		<div class="grp">
			{#each contact.phones as p, i (i)}
				{@const href = telHref(p.value)}
				<div class="frow">
					<span class="fx"
						><small>{typeLabel(p.type, p.label)}</small>{#if href}<a class="val tel" {href}
								>{p.value}</a
							>{:else}<span class="val plain">{p.value}</span>{/if}</span
					>{@render copyButton(`p${i}`, p.value, p.value)}
				</div>
			{/each}
		</div>
	{/if}
	{#if contact.emails.length}
		<div class="grp">
			{#each contact.emails as e, i (i)}
				<div class="frow">
					<span class="fx"
						><small>{typeLabel(e.type, e.label)}</small>{#if mail}<button
								type="button"
								class="val link-val"
								title={t('contacts.act.mailTo', { addr: e.value })}
								onclick={() => onmail(e.value.trim())}>{e.value}</button
							>{:else}<a class="val" href="mailto:{e.value.trim()}">{e.value}</a>{/if}</span
					>{@render copyButton(`e${i}`, e.value, e.value)}
				</div>
			{/each}
		</div>
	{/if}
	{#if contact.addresses.length}
		<div class="grp">
			{#each contact.addresses as a, i (i)}
				{@const lines = addressLines(a)}
				{@const map = mapHref(a, apple)}
				<div class="frow">
					<span class="fx"
						><small>{typeLabel(a.type, a.label)}</small><span class="lines plain"
							>{#each lines as line, j (j)}<span>{line}</span>{/each}</span
						></span
					>{#if map}<a
							class="ib"
							href={map}
							target="_blank"
							rel="noopener noreferrer"
							aria-label={t('contacts.act.mapShow')}
							title={t('contacts.act.mapShow')}
							data-contact-map><Icon name="place" size={18} /></a
						>{/if}{@render copyButton(`a${i}`, lines.join('\n'), t('contacts.field.address'))}
				</div>
			{/each}
		</div>
	{/if}
	{#if birthday}
		<div class="grp">
			<div class="frow">
				<span class="fx"
					><small>{t('contacts.field.birthday')}</small><span class="val plain">{birthday}</span
					></span
				>
			</div>
		</div>
	{/if}
	{#if web}
		<div class="grp">
			<div class="frow">
				<span class="fx"
					><small>{t('contacts.field.web')}</small><a
						class="val"
						href={web}
						target="_blank"
						rel="noopener noreferrer">{web}</a
					></span
				>
			</div>
		</div>
	{/if}
	{#if contact.notes.trim()}
		<div class="grp">
			<div class="frow">
				<span class="fx"
					><small>{t('contacts.field.notes')}</small><span class="pre plain"
						>{#each linkPieces(contact.notes) as piece, i (i)}{#if piece.href && safeHref(piece.href)}<a
									href={piece.href}
									target="_blank"
									rel="noopener noreferrer">{piece.text}</a
								>{:else}{piece.text}{/if}{/each}</span
					></span
				>
			</div>
		</div>
	{/if}
	{#if groups.length}
		<div class="grp" data-contact-groups>
			<div class="frow">
				<span class="fx"
					><small>{t('contacts.groups.title')}</small><span class="val plain"
						>{groups.map((g) => g.name || t('contacts.groups.unnamed')).join(', ')}</span
					></span
				>
			</div>
		</div>
	{/if}
	<div class="grp">
		<button type="button" class="arow" onclick={onevent} data-contact-event
			>{t('contacts.act.newEvent')}</button
		>
		{#if onme}
			<button type="button" class="arow" onclick={() => onme(!isMe)} data-contact-me
				>{isMe ? t('contacts.me.unset') : t('contacts.me.set')}</button
			>
		{/if}
		{#if inGroup && onleavegroup}
			<button type="button" class="arow" onclick={onleavegroup} data-contact-leave
				>{t('contacts.groups.removeFrom', { group: inGroup })}</button
			>
		{/if}
	</div>
	<p class="muted small foot">
		{t('contacts.inBook', { book })}{#if changed}{' · '}{t('contacts.modified', {
				date: changed,
			})}{/if}
	</p>
</article>

<style>
	.detail {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		width: 100%;
		max-width: 40rem;
		margin: 0 auto;
	}
	.nav {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		min-height: 44px;
		margin: 0 calc(-1 * var(--space-2));
	}
	.link {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		min-width: 44px;
		min-height: 44px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.compact .link {
		font-size: 1.0625rem;
	}
	.link.back {
		padding-left: 0;
	}
	.link:focus-visible,
	.act:focus-visible,
	.arow:focus-visible,
	.ib:focus-visible,
	.link-val:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.dh {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		text-align: center;
	}
	.dh h2 {
		margin: var(--space-1) 0 0;
		font-size: 1.75rem;
		font-weight: 600;
		line-height: 1.2;
		overflow-wrap: anywhere;
	}
	.org {
		margin: 0;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.acts {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: var(--space-2);
	}
	.act {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-1);
		min-width: 0;
		min-height: 60px;
		padding: var(--space-2) var(--space-1);
		border: 0;
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--surface);
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		text-align: center;
		text-decoration: none;
		cursor: pointer;
	}
	.identity {
		min-width: 0;
	}
	.compact .identity {
		display: contents;
	}
	.detail:not(.compact) .dh {
		flex-direction: row;
		gap: var(--space-3);
		text-align: start;
	}
	.detail:not(.compact) .acts {
		display: flex;
		flex-wrap: wrap;
	}
	.detail:not(.compact) .act {
		flex-direction: row;
		gap: var(--space-2);
		min-height: 38px;
		padding: var(--space-1) var(--space-3);
	}
	.detail:not(.compact) .dh .identity {
		flex: 1;
	}
	.act:hover:not(:disabled) {
		background: color-mix(in srgb, var(--accent) 10%, var(--surface));
	}
	.act.off {
		color: var(--muted);
		cursor: default;
	}
	/* Not only the colour: the dimmed button has no icon colour and a dashed edge. */
	.act.off .qi {
		opacity: 0.55;
	}
	.act.off {
		outline: 1px dashed var(--border);
		outline-offset: -1px;
	}
	.qi {
		display: inline-flex;
	}
	.ql {
		max-width: 100%;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.shared {
		margin: 0;
		text-align: center;
		overflow-wrap: anywhere;
	}
	.grp {
		display: flex;
		flex-direction: column;
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--surface);
		overflow: clip;
	}
	.frow {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 52px;
		padding: var(--space-2) var(--space-1) var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--border);
	}
	.frow:last-child {
		border-bottom: 0;
	}
	.fx {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.fx small {
		color: var(--text);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.val {
		align-self: flex-start;
		color: var(--accent);
		font-size: 1.0625rem;
		overflow-wrap: anywhere;
		text-decoration: none;
	}
	.val:hover {
		text-decoration: underline;
	}
	.plain {
		color: var(--text);
		font-size: 1.0625rem;
	}
	.link-val {
		padding: 0;
		border: 0;
		background: none;
		font: inherit;
		font-size: 1.0625rem;
		text-align: left;
		cursor: pointer;
	}
	.lines {
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.pre {
		white-space: pre-line;
		overflow-wrap: anywhere;
	}
	.pre a {
		color: var(--accent);
	}
	.cp {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
	}
	.done {
		color: var(--muted);
		font-size: var(--text-sm);
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
	.compact .ib {
		min-width: 44px;
		min-height: 44px;
	}
	.arow {
		display: flex;
		align-items: center;
		width: 100%;
		min-height: 48px;
		padding: var(--space-2) var(--space-4);
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: 1.0625rem;
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.arow:last-child {
		border-bottom: 0;
	}
	.arow:hover {
		background: color-mix(in srgb, var(--accent) 8%, var(--surface));
	}
	.small {
		font-size: var(--text-sm);
	}
	.foot {
		margin: 0;
		text-align: center;
		overflow-wrap: break-word;
	}
	/* A number to tap: 44 px high on a touch screen, the row stays compact. */
	.compact .val.tel,
	.compact a.val,
	.compact .link-val {
		display: inline-flex;
		align-items: center;
		min-height: 44px;
		margin: -10px 0;
	}
</style>
