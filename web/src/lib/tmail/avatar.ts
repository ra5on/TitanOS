// 1.12 (owner 18 Nov 2026, "Absenderbild"): the initials of a sender in a
// colour of its own that stays the same for the same address – one of the
// icon-square colours of the theme, on which white text (--on-icon-square)
// reaches 4.5:1 in every theme.
import { initialsOf } from '../pim/contacts.ts';

export const AVATAR_COLORS = [
	'blue',
	'violet',
	'teal',
	'cyan',
	'green',
	'orange',
	'amber',
	'red',
	'pink',
	'slate',
] as const;

/** The CSS variable of an address's colour (FNV-1a over the lower-case address). */
export function avatarColor(addr: string): string {
	let h = 0x811c9dc5;
	for (const ch of (addr ?? '').trim().toLowerCase()) {
		h ^= ch.codePointAt(0)!;
		h = Math.imul(h, 0x01000193) >>> 0;
	}
	return `var(--icon-square-${AVATAR_COLORS[h % AVATAR_COLORS.length]})`;
}

/** Up to two letters of a name, else of the address before the "@". */
export function avatarInitials(name: string, addr: string): string {
	if ((name ?? '').trim()) return initialsOf(name);
	const local = (addr ?? '').split('@')[0].replace(/[^\p{L}\p{N}]+/gu, ' ').trim();
	return local ? initialsOf(local) : '?';
}
