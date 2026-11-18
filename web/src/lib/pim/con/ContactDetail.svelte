<script lang="ts">
	// 1.11 (step 16): one contact (drafts 10, 51): picture, name, company,
	// "Anrufen" (tel:), "E-Mail schreiben" (T-Mail, only with its release)
	// and "Termin" (a new event with the name), then the numbers, e-mail
	// addresses, addresses, birthday, web address and note – each with its
	// kind in words, numbers as tel: links, everything as text. Links only
	// http(s)/mailto/tel, with rel="noopener noreferrer" and their target
	// as the visible text.
	// 1.13 (owner 18 Nov 2026): the groups of the contact; each e-mail
	// address opens Mail's compose in a window (only with Mail's release,
	// else a mailto: link); compact (the phone) the big round actions of
	// iOS's contacts – Anrufen, Nachricht (sms:), E-Mail, Karte – edge to
	// edge, editing from the bar at the bottom.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { linkPieces, safeHref } from '../calendar.ts';
	import { dateText } from '../calendar-text.ts';
	import {
		addressLines,
		mapHref,
		smsHref,
		telHref,
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
		phone: boolean;
		/** T-Mail is released for the account: "E-Mail schreiben" shows. */
		mail: boolean;
		/** The name of the address book ("Kontakte"). */
		book: string;
		/** The groups the contact is in. */
		groups?: Group[];
		onedit: () => void;
		onmail: (addr: string) => void;
		onevent: () => void;
	};
	let {
		contact,
		soon,
		today,
		phone,
		mail,
		book,
		groups = [],
		onedit,
		onmail,
		onevent,
	}: Props = $props();

	const call = $derived(contact.phones.map((p) => telHref(p.value)).find(Boolean) ?? '');
	// A message goes to a mobile number first.
	const sms = $derived(
		[...contact.phones.filter((p) => p.type === 'mobile'), ...contact.phones]
			.map((p) => smsHref(p.value))
			.find(Boolean) ?? '',
	);
	const apple =
		typeof navigator !== 'undefined' && /iPhone|iPad|Macintosh/.test(navigator.userAgent);
	const map = $derived(contact.addresses.map((a) => mapHref(a, apple)).find(Boolean) ?? '');
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
</script>

{#snippet copyButton(key: string, text: string, what: string)}
	<span class="cp">
		{#if copied === key}<small class="done" role="status">{t('contacts.copied')}</small>{/if}
		<button
			type="button"
			class="ib"
			aria-label={t('contacts.copy', { what })}
			title={t('contacts.copy', { what })}
			onclick={() => copy(key, text)}><Icon name="copy" size={15} /></button
		>
	</span>
{/snippet}

<article class="detail" class:phone data-contact-detail={contact.id}>
	<header class="dh">
		<Avatar
			id={contact.id}
			name={contact.name}
			org={contact.org}
			hasPhoto={contact.hasPhoto}
			version={contact.etag}
			size={phone ? 88 : 72}
		/>
		<div class="dn">
			<h2>{title}</h2>
			{#if contact.org && contact.org !== contact.name}<p class="muted">{contact.org}</p>{/if}
		</div>
		{#if !phone}<button type="button" class="btn" onclick={onedit}
				><Icon name="edit" size={15} />{t('contacts.edit')}</button
			>{/if}
	</header>
	{#if groups.length}
		<p class="chips" data-contact-groups>
			<span class="sr-only">{t('contacts.groups.title')}:</span>
			{#each groups as g (g.id)}<span class="chip"
					><Icon name="users" size={13} />{g.name || t('contacts.groups.unnamed')}</span
				>{/each}
		</p>
	{/if}
	<div class="acts" data-contact-actions>
		{#if call}<a class="btn act" href={call}
				><span class="ico"><Icon name="phone" size={18} /></span>{t('contacts.act.call')}</a
			>{/if}
		{#if phone && sms}<a class="btn act" href={sms}
				><span class="ico"><Icon name="chat" size={18} /></span>{t('contacts.act.sms')}</a
			>{/if}
		{#if firstMail}
			{#if mail}<button type="button" class="btn act" onclick={() => onmail(firstMail)}
					><span class="ico"><Icon name="mail" size={18} /></span>{phone
						? t('contacts.act.mailShort')
						: t('contacts.act.mail')}</button
				>{:else}<a class="btn act" href="mailto:{firstMail}"
					><span class="ico"><Icon name="mail" size={18} /></span>{phone
						? t('contacts.act.mailShort')
						: t('contacts.act.mail')}</a
				>{/if}
		{/if}
		{#if phone && map}<a class="btn act" href={map} target="_blank" rel="noopener noreferrer"
				><span class="ico"><Icon name="place" size={18} /></span>{t('contacts.act.map')}</a
			>{/if}
		{#if !phone}<button type="button" class="btn act" onclick={onevent}
				><span class="ico"><Icon name="calendar" size={18} /></span>{t(
					'contacts.act.event',
				)}</button
			>{/if}
	</div>
	<div class="fields">
		{#each contact.phones as p, i (i)}
			{@const href = telHref(p.value)}
			<div class="frow">
				<Icon name="phone" size={18} /><span class="fx"
					><small>{t('contacts.field.phone')} · {typeLabel(p.type, p.label)}</small>{#if href}<a
							class="tel"
							{href}>{p.value}</a
						>{:else}<span class="val">{p.value}</span>{/if}</span
				>{@render copyButton(`p${i}`, p.value, p.value)}
			</div>
		{/each}
		{#each contact.emails as e, i (i)}
			<div class="frow">
				<Icon name="mail" size={18} /><span class="fx"
					><small>{t('contacts.field.email')} · {typeLabel(e.type, e.label)}</small
					>{#if mail}<button
							type="button"
							class="val link"
							title={t('contacts.act.mailTo', { addr: e.value })}
							onclick={() => onmail(e.value.trim())}>{e.value}</button
						>{:else}<a class="val" href="mailto:{e.value.trim()}">{e.value}</a>{/if}</span
				>{@render copyButton(`e${i}`, e.value, e.value)}
			</div>
		{/each}
		{#each contact.addresses as a, i (i)}
			{@const lines = addressLines(a)}
			<div class="frow">
				<Icon name="place" size={18} /><span class="fx"
					><small>{t('contacts.field.address')} · {typeLabel(a.type, a.label)}</small><span
						class="lines"
						>{#each lines as line, j (j)}<span>{line}</span>{/each}</span
					>{#if !phone && mapHref(a, apple)}<a
							class="maplink"
							href={mapHref(a, apple)}
							target="_blank"
							rel="noopener noreferrer">{t('contacts.act.mapShow')}</a
						>{/if}</span
				>{@render copyButton(`a${i}`, lines.join('\n'), t('contacts.field.address'))}
			</div>
		{/each}
		{#if birthday}
			<div class="frow">
				<Icon name="cake" size={18} /><span class="fx"
					><small>{t('contacts.field.birthday')}</small><span>{birthday}</span></span
				>
			</div>
		{/if}
		{#if web}
			<div class="frow">
				<Icon name="globe" size={18} /><span class="fx"
					><small>{t('contacts.field.web')}</small><a
						class="val"
						href={web}
						target="_blank"
						rel="noopener noreferrer">{web}</a
					></span
				>
			</div>
		{/if}
		{#if contact.notes.trim()}
			<div class="frow">
				<Icon name="note" size={18} /><span class="fx"
					><small>{t('contacts.field.notes')}</small><span class="pre"
						>{#each linkPieces(contact.notes) as piece, i (i)}{#if piece.href && safeHref(piece.href)}<a
									href={piece.href}
									target="_blank"
									rel="noopener noreferrer">{piece.text}</a
								>{:else}{piece.text}{/if}{/each}</span
					></span
				>
			</div>
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
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		max-width: 46rem;
	}
	.dh {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	.phone .dh {
		flex-direction: column;
		text-align: center;
	}
	.dn {
		flex: 1;
		min-width: 12rem;
	}
	.phone .dn {
		min-width: 0;
		width: 100%;
	}
	.dn h2 {
		margin: 0;
		font-size: var(--text-xl);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.dn p {
		margin: 2px 0 0;
		overflow-wrap: break-word;
	}
	.dh .btn,
	.act {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.act {
		min-height: 40px;
		text-decoration: none;
	}
	/* The phone: iOS's round actions, side by side, the name below. */
	.phone .acts {
		display: grid;
		grid-auto-flow: column;
		grid-auto-columns: minmax(0, 1fr);
		gap: var(--space-2);
	}
	.phone .act {
		flex-direction: column;
		justify-content: center;
		gap: var(--space-1);
		min-height: 72px;
		padding: var(--space-2) var(--space-1);
		border-radius: var(--radius-tile);
		text-align: center;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.phone .act .ico {
		display: inline-grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
	}
	.ico {
		display: inline-flex;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		margin: 0;
	}
	.phone .chips {
		justify-content: center;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px var(--space-2);
		border-radius: 999px;
		background: var(--chip);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.link {
		padding: 0;
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		text-align: left;
		text-decoration: underline;
		cursor: pointer;
	}
	.maplink {
		align-self: flex-start;
		font-size: var(--text-sm);
	}
	.fields {
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.frow {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		min-height: 52px;
		padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
		border-radius: var(--srow-radius, 6px);
		background: var(--srow-bg);
	}
	.frow > :global(svg) {
		flex: none;
		margin-top: 9px;
		color: var(--muted);
	}
	.fx {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 1px;
	}
	.fx small {
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.fx a {
		color: var(--accent);
		overflow-wrap: anywhere;
	}
	/* A number to tap: 44 px high on the phone, the row stays compact. */
	.phone .fx a.tel,
	.phone .fx a.val,
	.phone .fx .link {
		display: inline-flex;
		align-items: center;
		align-self: flex-start;
		min-height: 44px;
		margin: -10px 0;
	}
	.val {
		overflow-wrap: anywhere;
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
	.small {
		font-size: var(--text-sm);
	}
	.foot {
		margin: 0;
		overflow-wrap: break-word;
	}
</style>
