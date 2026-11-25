import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, within } from '@testing-library/svelte';
import { t } from '#lib/i18n/index.svelte.ts';
import HomePage from '#lib/components/agent/pages/HomePage.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

test('the first screen shows suggestions and the next task together, including on a fresh phone chat', async () => {
	const next = new Date(Date.now() + 3600000).toISOString();
	vi.stubGlobal('fetch', async (url: string) => {
		if (url.endsWith('/schedules'))
			return reply(200, {
				schedules: [{ id: 's1', enabled: true, name: 'Dokumente sichern', next }],
			});
		if (url.endsWith('/home')) return reply(200, { state: null });
		return reply(200, { items: [] });
	});
	const onfill = vi.fn(),
		onask = vi.fn(),
		onshow = vi.fn();
	const { getByRole, queryByRole, rerender } = render(HomePage, {
		user: 'alex',
		chats: [],
		suggestions: [
			{ title: 'Dateien finden', text: 'Suche meine Dateien', sub: 'Dokumente auf dem NAS' },
		],
		phone: true,
		fresh: true,
		onfill,
		onask,
		onshow,
		onopen: vi.fn(),
		onapproval: vi.fn(),
		onstop: vi.fn(),
	});
	const idea = getByRole('button', { name: /Dateien finden/ });
	const overview = within(getByRole('region', { name: t('agent.night.overview') }));
	const task = await overview.findByRole('button', { name: /Dokumente sichern/ });
	await fireEvent.click(idea);
	expect(onfill).toHaveBeenCalledWith('Suche meine Dateien');
	expect(onask).not.toHaveBeenCalled();
	await fireEvent.click(task);
	expect(onshow).toHaveBeenCalledWith('planned');
	const details = overview.getByRole('button', { expanded: false });
	await fireEvent.click(details);
	expect(details.getAttribute('aria-expanded')).toBe('true');
	expect(getByRole('button', { name: /Dateien finden/ })).toBe(idea);
	await rerender({ show: false });
	expect(queryByRole('region', { name: t('agent.night.ideas') })).toBeNull();
	expect(overview.getByRole('button', { name: /Dokumente sichern/ })).toBeTruthy();
});
