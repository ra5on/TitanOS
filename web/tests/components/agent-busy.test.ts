import { expect, test } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import ChatView from '#lib/components/agent/ChatView.svelte';
import { busyKind } from '#lib/agent.ts';

// jsdom has no scrolling.
Element.prototype.scrollTo = () => {};
// … and no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

const at = '2026-11-15T20:00:00Z';
const chat = (entries: object[]) =>
	({ id: 'c1', title: 'Test', created: at, updated: at, entries }) as never;
const base = { ondecide: () => {}, onchoice: () => {} };
const step = (tool: string) => ({ id: 's1', role: 'tool', tool, at, args: { query: 'titanos' } });

test('each kind of step has its figure, the rest keeps thinking', () => {
	expect(busyKind('web_search')).toBe('web');
	expect(busyKind('web_read')).toBe('web');
	expect(busyKind('files_list')).toBe('files');
	expect(busyKind('document_read')).toBe('files');
	expect(busyKind('document_edit')).toBe('write');
	expect(busyKind('file_write')).toBe('write');
	expect(busyKind('ha_state')).toBe('home');
	expect(busyKind('ha_call')).toBe('switch');
	expect(busyKind('code_run')).toBe('code');
	expect(busyKind('docker_list')).toBe('system');
	expect(busyKind('storage_overview')).toBe('system');
	expect(busyKind('tailscale_status')).toBe('');
	expect(busyKind(undefined)).toBe('');
});

test('a running web search shows the web figure and a word of its kind', () => {
	const { container } = render(ChatView, {
		...base,
		chat: chat([{ id: 'u1', role: 'user', content: 'Suche', at }]),
		busy: true,
		live: { thinking: '', content: '', step: step('web_search') as never },
	});
	expect(container.querySelector('.fig')?.getAttribute('data-kind')).toBe('web');
	expect(container.querySelectorAll('.fig i').length).toBe(3);
	const line = container.querySelector('.live-line .sweep')?.textContent ?? '';
	expect(line).toContain('titanos');
	expect(line).not.toContain('agent.busy');
	expect(line).toMatch(/ …/);
});

test('an unknown step keeps its name, thinking shows a word', () => {
	const { container, rerender } = render(ChatView, {
		...base,
		chat: chat([]),
		busy: true,
		live: { thinking: '', content: '', step: step('tailscale_status') as never },
	});
	expect(container.querySelector('.fig')?.getAttribute('data-kind')).toBe('');
	expect(container.querySelector('.sweep')?.textContent).not.toMatch(/ …/);
	void rerender({ live: { thinking: '', content: '', step: null } });
	flushSync();
	expect(container.querySelector('.busy-word')?.textContent).toMatch(/ …$/);
});

test('the tick shows only after a run that ended with an answer', async () => {
	const entries: object[] = [{ id: 'u1', role: 'user', content: 'Hallo', at }];
	const { container, rerender } = render(ChatView, {
		...base,
		chat: chat(entries),
		busy: false,
		live: { thinking: '', content: '', step: null },
	});
	expect(container.querySelector('.live-line.done')).toBeNull();
	await rerender({ busy: true });
	expect(container.querySelector('.live-line.done')).toBeNull();
	await rerender({
		chat: chat([...entries, { id: 'a1', role: 'assistant', content: 'Hallo!', at }]),
		busy: false,
	});
	expect(container.querySelector('.live-line.done .tick')).not.toBeNull();
	// A run that ends without an answer has no tick.
	await rerender({ busy: true });
	await rerender({ chat: chat(entries), busy: false });
	expect(container.querySelector('.live-line.done')).toBeNull();
});
