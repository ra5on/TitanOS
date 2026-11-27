import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, waitFor, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import WidgetView from '#lib/desktop/WidgetView.svelte';
import { session } from '#lib/session.svelte.ts';
import { reply } from './fetch.ts';

function signIn(name: string) {
	session.data = {
		configAvailable: true,
		setupRequired: false,
		hostname: 'test',
		user: { name, role: 'user', twoFactor: false },
	};
}
function activity(id: string, title: string, status: string) {
	return { id, title, status, updated: '2026-11-17T10:00:00Z' };
}
beforeEach(() => {
	localStorage.clear();
	signIn('mia');
});
afterEach(() => {
	vi.unstubAllGlobals();
	session.data = null;
});

test('only personal activity is fetched and an approval opens the exact job', async () => {
	const calls: string[] = [];
	const job = activity('waiting1', 'Termin prüfen', 'waiting');
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			calls.push(String(url));
			return reply(200, { jobs: [job] });
		}),
	);
	const open = vi.fn();
	const view = render(WidgetView, { props: { kind: 'agent', onopen: open } });
	await fireEvent.click(await view.findByText(job.title));
	expect(open).toHaveBeenCalledWith('app:agent', '/agent?job=waiting1');
	expect(calls.length).toBeGreaterThan(0);
	expect(calls.every((x) => x.endsWith('/personal-agent/activity'))).toBe(true);
});

test('an account switch discards a late response and the unsent prompt', async () => {
	let late!: (value: Response) => void;
	let queued = 0;
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => {
			queued++;
			if (queued === 1) return reply(200, { jobs: [activity('a', 'Mias Auftrag', 'running')] });
			if (queued === 2) return new Promise<Response>((resolve) => (late = resolve));
			return reply(200, { jobs: [activity('c', 'Annas Ergebnis', 'done')] });
		}),
	);
	const view = render(WidgetView, { props: { kind: 'agent', onopen: () => {} } });
	await view.findByText('Mias Auftrag');
	await fireEvent.input(view.getByRole('textbox'), { target: { value: 'Mein privater Entwurf' } });
	signIn('tom');
	await waitFor(() => expect(queued).toBe(2));
	expect(view.queryByText('Mias Auftrag')).toBeNull();
	signIn('anna');
	await view.findByText('Annas Ergebnis');
	late(reply(200, { jobs: [activity('b', 'Toms Auftrag', 'running')] }));
	await tick();
	expect(view.queryByText('Toms Auftrag')).toBeNull();
	expect((view.getByRole('textbox') as HTMLInputElement).value).toBe('');
	session.data = null;
	await waitFor(() => expect(view.queryByText('Annas Ergebnis')).toBeNull());
});

test('an empty or unreachable personal account never reads the old profile', async () => {
	localStorage.setItem('titanos.agent.look', JSON.stringify({ dotColor: '#ff0000' }));
	const calls: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			calls.push(String(url));
			return calls.length === 1
				? reply(200, { jobs: [] })
				: reply(503, { error: { code: 'agent_storage_unavailable' } });
		}),
	);
	const view = render(WidgetView, { props: { kind: 'agent', onopen: () => {} } });
	await view.findByRole('textbox');
	expect(view.container.querySelector('[data-agent-figure]')).toBeNull();
	signIn('anna');
	await waitFor(() => expect(calls.length).toBe(2));
	await waitFor(() => expect(view.queryByRole('textbox')).toBeNull());
	expect(calls.every((x) => x.endsWith('/personal-agent/activity'))).toBe(true);
});
