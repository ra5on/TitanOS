import { onMount } from 'svelte';
import { registerMenuCommands } from '../menu-commands.svelte.ts';
import type { Chat } from './api.ts';
type Tab = 'chat' | 'jobs' | 'settings';
export function personalMenu(s: {
	ready: () => boolean;
	modal: () => boolean;
	chats: () => Chat[];
	selected: () => string;
	tab: () => Tab;
	show: (tab: Tab) => void;
	newChat: () => void;
	openChat: (id: string) => void;
}) {
	const available = () => s.ready() && !s.modal();
	function step(delta: number) {
		const chats = s.chats(),
			active = chats.findIndex((c) => c.id === s.selected());
		return chats[active < 0 ? chats.length - 1 : active + delta]?.id;
	}
	function move(delta: number) {
		const id = step(delta);
		if (id) s.openChat(id);
	}
	const tab = (name: Tab) => ({
		enabled: available,
		checked: () => s.tab() === name,
		run: () => s.show(name),
	});
	registerMenuCommands({
		'agent.newChat': { enabled: available, run: s.newChat },
		'agent.chat': tab('chat'),
		'agent.jobs': tab('jobs'),
		'agent.settings': tab('settings'),
		'agent.chats': {
			enabled: available,
			items: () =>
				s
					.chats()
					.slice()
					.reverse()
					.map((c) => ({ id: c.id, text: c.title, checked: c.id === s.selected() })),
			run: (id) => {
				if (id && s.chats().some((c) => c.id === id)) s.openChat(id);
			},
		},
		'agent.previousChat': { enabled: () => available() && !!step(-1), run: () => move(-1) },
		'agent.nextChat': { enabled: () => available() && !!step(1), run: () => move(1) },
	});
	onMount(() => {
		function key(e: KeyboardEvent) {
			if (
				e.defaultPrevented ||
				e.repeat ||
				e.isComposing ||
				!e.altKey ||
				!e.shiftKey ||
				e.metaKey ||
				e.ctrlKey ||
				!available()
			)
				return;
			switch (e.code) {
				case 'KeyN':
					s.newChat();
					break;
				case 'Digit1':
					s.show('chat');
					break;
				case 'Digit2':
					s.show('jobs');
					break;
				case 'Digit3':
					s.show('settings');
					break;
				case 'ArrowUp':
					if (!step(-1)) return;
					move(-1);
					break;
				case 'ArrowDown':
					if (!step(1)) return;
					move(1);
					break;
				default:
					return;
			}
			e.preventDefault();
		}
		window.addEventListener('keydown', key);
		return () => window.removeEventListener('keydown', key);
	});
}
