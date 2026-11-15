import { afterEach, expect, test, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import VoiceMode from '#lib/components/agent/VoiceMode.svelte';
import { reply } from './fetch.ts';

// jsdom has no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

// A microphone whose loudness the test sets: quiet, speech, quiet again.
let loud = 0;
class FakeContext {
	createAnalyser() {
		return {
			fftSize: 1024,
			getFloatTimeDomainData(samples: Float32Array) {
				samples.fill(loud);
			},
		};
	}
	createMediaStreamSource() {
		return { connect() {} };
	}
	close() {
		return Promise.resolve();
	}
}
class FakeRecorder {
	static isTypeSupported = () => true;
	state = 'inactive';
	mimeType = 'audio/webm';
	ondataavailable: ((e: { data: Blob }) => void) | null = null;
	onstop: (() => void) | null = null;
	start() {
		this.state = 'recording';
	}
	stop() {
		this.state = 'inactive';
		this.ondataavailable?.({ data: new Blob(['sound'], { type: 'audio/webm' }) });
		this.onstop?.();
	}
}
const stopped = vi.fn();
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
const at = '2026-11-15T20:00:00Z';
const chat = (entries: object[]) =>
	({ id: 'c1', title: 'Test', created: at, updated: at, entries }) as never;

afterEach(() => vi.unstubAllGlobals());

test('speech after a pause is sent, the answer shown, then it listens again', async () => {
	vi.stubGlobal('AudioContext', FakeContext);
	vi.stubGlobal('MediaRecorder', FakeRecorder);
	vi.stubGlobal('requestAnimationFrame', (f: () => void) => setTimeout(f, 10));
	vi.stubGlobal('cancelAnimationFrame', (id: number) => clearTimeout(id));
	Object.defineProperty(navigator, 'mediaDevices', {
		configurable: true,
		value: { getUserMedia: async () => ({ getTracks: () => [{ stop: stopped }] }) },
	});
	const asked: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			asked.push(String(url));
			return reply(200, { text: 'Stehlampe an' });
		}),
	);
	const onsend = vi.fn();
	const user = { id: 'u1', role: 'user', content: 'Stehlampe an', at };
	const { container, rerender, unmount } = render(VoiceMode, {
		chat: chat([]),
		busy: false,
		onsend,
		onclose: () => {},
	});
	const state = () => container.querySelector('.state')?.textContent;
	await wait(60);
	expect(state()).toBe('Listening');
	// Quiet: nothing is recorded. Speech: it is.
	await wait(100);
	expect(onsend).not.toHaveBeenCalled();
	loud = 0.3;
	await wait(500);
	expect(state()).toBe('I hear you …');
	// A pause ends it; the text goes to the agent.
	loud = 0;
	await wait(1600);
	expect(asked.some((u) => u.includes('/agent/transcribe'))).toBe(true);
	expect(onsend).toHaveBeenCalledWith('Stehlampe an');
	expect(state()).toBe('Thinking …');
	expect(container.querySelector('.heard')?.textContent).toBe('Stehlampe an');
	// The run ends with an answer: it is shown (jsdom has no voice), and
	// the microphone listens again.
	await rerender({ busy: true });
	await rerender({
		chat: chat([user, { id: 'a1', role: 'assistant', content: '**Stehlampe** ist an.', at }]),
		busy: false,
	});
	await wait(30);
	expect(container.querySelector('.answer')?.textContent).toBe('Stehlampe ist an.');
	expect(state()).toBe('Listening');
	// A step that waits for the person stops the conversation.
	loud = 0.3;
	await wait(400);
	loud = 0;
	await wait(1600);
	await rerender({ busy: true });
	await rerender({
		chat: { ...(chat([user]) as object), pending: { id: 'p1', tool: 'ha_call' } } as never,
		busy: false,
	});
	expect(state()).toBe('Paused');
	expect(container.querySelector('.problem')?.textContent).toContain('approval');
	unmount();
	expect(stopped).toHaveBeenCalled();
}, 15000);
