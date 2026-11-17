import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, waitFor } from '@testing-library/svelte';
import WidgetView from '#lib/desktop/WidgetView.svelte';
import { agentLook } from '#lib/agent-look.svelte.ts';
import { LOOK_KEY } from '#lib/agent.ts';
import { reply } from './fetch.ts';

// 1.06: the three dots of the desktop widget were a fixed orange and size.
// They are the chat's figure now, in the colour and size of the agent's
// settings, without asking the NAS when this browser has the agent's copy.

const running = {
	id: 'c1',
	title: 'Bericht schreiben',
	state: 'running',
	updated: '2026-11-17T10:00:00Z',
};

function stubNas(prefs: Record<string, unknown>) {
	const asked: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			asked.push(String(url));
			if (String(url).endsWith('/agent/status'))
				return reply(200, { enabled: true, allowed: true });
			if (String(url).endsWith('/agent/chats')) return reply(200, { chats: [running] });
			if (String(url).endsWith('/agent/prefs')) return reply(200, prefs);
			return reply(404, { error: { code: 'not_found', message: '' } });
		}),
	);
	return asked;
}

beforeEach(() => {
	localStorage.clear();
	agentLook.reset();
});
afterEach(() => vi.unstubAllGlobals());

test('a running chat shows the figure in the colour and size of this browser’s copy', async () => {
	localStorage.setItem(
		LOOK_KEY,
		JSON.stringify({ dotColor: '#2E7D32', dotSize: 'l', motion: 'full' }),
	);
	const asked = stubNas({ dotColor: '#000000' });
	const { container } = render(WidgetView, { props: { kind: 'agent', onopen: () => {} } });
	await waitFor(() => expect(container.querySelector('[data-agent-figure] .fig')).not.toBeNull());
	const figure = container.querySelector<HTMLElement>('[data-agent-figure]')!;
	expect(figure.style.getPropertyValue('--ag-dots')).toBe('#2e7d32');
	expect(figure.style.getPropertyValue('--ag-dot-size')).toBe('26px');
	expect(figure.querySelectorAll('.fig i').length).toBe(3);
	expect(container.querySelector('.ag-dots')).toBeNull();
	// The copy is there: the NAS is not asked.
	expect(asked.some((u) => u.endsWith('/agent/prefs'))).toBe(false);
});

test('without a copy the account’s look is asked for once, and changes of the agent window follow', async () => {
	const asked = stubNas({ dotColor: '#1565c0', dotSize: 's' });
	const { container } = render(WidgetView, { props: { kind: 'agent', onopen: () => {} } });
	await waitFor(() =>
		expect(
			container
				.querySelector<HTMLElement>('[data-agent-figure]')
				?.style.getPropertyValue('--ag-dots'),
		).toBe('#1565c0'),
	);
	const figure = container.querySelector<HTMLElement>('[data-agent-figure]')!;
	expect(figure.style.getPropertyValue('--ag-dot-size')).toBe('14px');
	expect(asked.filter((u) => u.endsWith('/agent/prefs')).length).toBe(1);
	// The agent window (a document of its own) saves another look.
	localStorage.setItem(LOOK_KEY, JSON.stringify({ dotColor: '#c62828', dotSize: 'm' }));
	window.dispatchEvent(new StorageEvent('storage', { key: LOOK_KEY }));
	await waitFor(() => expect(figure.style.getPropertyValue('--ag-dots')).toBe('#c62828'));
	expect(figure.style.getPropertyValue('--ag-dot-size')).toBe('18px');
	expect(asked.filter((u) => u.endsWith('/agent/prefs')).length).toBe(1);
});

test('no running chat, no question about the look', async () => {
	const asked: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			asked.push(String(url));
			if (String(url).endsWith('/agent/status'))
				return reply(200, { enabled: true, allowed: true });
			return reply(200, { chats: [] });
		}),
	);
	render(WidgetView, { props: { kind: 'agent', onopen: () => {} } });
	await waitFor(() => expect(asked.some((u) => u.endsWith('/agent/chats'))).toBe(true));
	await new Promise((r) => setTimeout(r, 50));
	expect(asked.some((u) => u.endsWith('/agent/prefs'))).toBe(false);
});
