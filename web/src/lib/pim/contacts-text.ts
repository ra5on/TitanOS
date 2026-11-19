// 1.11 (step 16): Kontakte in words – kinds of numbers and addresses,
// birthdays of this week, counts – in the language of the UI.
import { t, type MessageKey } from '../i18n/index.svelte.ts';
import { dateText, shortDate, weekdayLong } from './calendar-text.ts';
import { parseBirthday, soonWhen, type DuplicateSet, type Soon } from './contacts.ts';

/** "Mobil", "Privat", "Arbeit", "Sonstige" – or the card's own label ("Werkstatt"). */
export function typeLabel(type: string, label?: string): string {
	if (label) return label;
	const known = ['mobile', 'home', 'work', 'other'].includes(type) ? type : 'other';
	return t(`contacts.type.${known}` as MessageKey);
}

/** A birthday of this week in the list: "Geburtstag am Freitag", "hat heute Geburtstag". */
export function soonText(soon: Soon, today: string): string {
	const { when } = soonWhen(soon, today);
	if (when === 'today') return t('contacts.bday.today');
	const day = weekdayLong(soon.date);
	return when === 'coming' ? t('contacts.bday.coming', { day }) : t('contacts.bday.past', { day });
}

/**
 * The birthday of a contact: "20. November 1986 · wird am Freitag 40",
 * "20. November · Geburtstag am Freitag" (without year), or just the date.
 */
export function birthdayLine(value: string, soon: Soon | null, today: string): string {
	const b = parseBirthday(value);
	if (!b) return '';
	const mmdd = `${String(b.month).padStart(2, '0')}-${String(b.day).padStart(2, '0')}`;
	const date = b.year !== null ? dateText(`${b.year}-${mmdd}`) : shortDate(`2000-${mmdd}`);
	if (!soon) return date;
	// A card born this week (age 0) says no age (115-7).
	if (soon.age === undefined || soon.age < 1) return `${date} · ${soonText(soon, today)}`;
	const { when } = soonWhen(soon, today);
	const day = weekdayLong(soon.date);
	const age = soon.age;
	const turns =
		when === 'today'
			? t('contacts.bday.turnsToday', { age })
			: when === 'coming'
				? t('contacts.bday.turnsComing', { day, age })
				: t('contacts.bday.turnsPast', { day, age });
	return `${date} · ${turns}`;
}

/** "10 Kontakte". */
export function countText(n: number): string {
	return n === 1 ? t('contacts.count.one') : t('contacts.count.other', { n });
}

/** "4 Kontakte und 1 Vorschlag aus T-Mail für „beispiel“". */
export function foundText(contacts: number, suggestions: number, q: string): string {
	const c = countText(contacts);
	if (!suggestions) return t('contacts.found', { contacts: c, q });
	const s =
		suggestions === 1
			? t('contacts.sugCount.one')
			: t('contacts.sugCount.other', { n: suggestions });
	return t('contacts.foundBoth', { contacts: c, suggestions: s, q });
}

/** "14 gesendete Mails". */
export function sentText(n: number): string {
	return n === 1 ? t('contacts.sug.sentOne') : t('contacts.sug.sent', { n });
}

/** Why a set of contacts may be one person: "gleiche E-Mail-Adresse anna@… · gleicher Name". */
export function dupText(s: Pick<DuplicateSet, 'matches'>): string {
	return s.matches
		.map((m) => t(`contacts.dup.by.${m.kind}` as MessageKey, { value: m.value }))
		.join(' · ');
}
