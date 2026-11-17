// 1.03 (D102-48): a double click on "Jetzt aktualisieren" sent two updates.
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import DdnsStatus from '#lib/ddns/DdnsStatus.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const status = {
	settings: {
		enabled: true,
		hostnames: ['nas.example.org'],
		provider: 'duckdns',
		ipv4: true,
		ipv6: false,
		heartbeat: {},
	},
	secrets: {},
	running: false,
	heartbeat: {},
};

test('one update at a time', async () => {
	i18n.locale = 'de';
	let answer: (r: Response) => void = () => {};
	const calls: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string) => {
			calls.push(String(url));
			return new Promise<Response>((r) => (answer = r));
		}),
	);
	const onstatus = vi.fn();
	render(DdnsStatus, {
		props: { status: status as never, intro: '', onstatus, onrefresh: () => {} },
	});
	const button = screen.getByRole('button', { name: 'Jetzt aktualisieren' });
	await fireEvent.click(button);
	await fireEvent.click(button);
	expect(calls).toHaveLength(1);
	answer(reply(200, status));
	await waitFor(() => expect(onstatus).toHaveBeenCalledTimes(1));
	expect((button as HTMLButtonElement).disabled).toBe(false);
});
