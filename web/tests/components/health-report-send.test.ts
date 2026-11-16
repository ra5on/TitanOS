// 1.03 (D102-45): "Jetzt senden" in the health report kept nothing of a
// schedule changed but not saved yet; the form went back to the stored one.
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import HealthReport from '#lib/components/HealthReport.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const answer = {
	settings: { enabled: true, every: 'weekly', weekday: 1, day: 1, time: '08:00' },
	preview: { title: 'Bericht', status: 'ok', sections: [] },
};

test('sending the report keeps a schedule not saved yet', async () => {
	i18n.locale = 'de';
	const sent: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init?: RequestInit) => {
			if (init?.method === 'POST') sent.push(String(url));
			return reply(200, answer);
		}),
	);
	render(HealthReport);
	const row = await screen.findByRole('button', { expanded: false });
	await fireEvent.click(row);
	const time = (await screen.findByLabelText('Uhrzeit')) as HTMLInputElement;
	await waitFor(() => expect(time.value).toBe('08:00'));
	await fireEvent.input(time, { target: { value: '19:30' } });
	await fireEvent.click(screen.getByRole('button', { name: 'Jetzt schicken' }));
	await waitFor(() => expect(sent).toHaveLength(1));
	await screen.findByRole('status');
	expect(time.value).toBe('19:30');
});
