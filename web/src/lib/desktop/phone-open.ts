import { goto } from '$app/navigation';
import type { Program } from './programs.svelte.ts';
import { markOpening } from './phone-motion.ts';

export function openPhoneProgram(program: Program, icon?: HTMLElement | null) {
	markOpening(program.key, icon ?? null);
	if (program.kind === 'webapp')
		return goto('/', { state: { webapp: { id: program.key.slice(7) } } });
	if (program.kind === 'docker') return goto('/', { state: { remote: program.key } });
	return goto(program.key === 'app:mail' ? '/mail?a=*&f=inbox' : program.href);
}
