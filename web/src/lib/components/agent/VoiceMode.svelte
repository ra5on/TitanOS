<script lang="ts">
	// Sprechmodus (0.95, owner 15 Nov 2026): a conversation without typing.
	// The microphone stays open; what is said is cut out by its loudness
	// (speech starts above the room's own noise, ends after a pause), goes
	// to speech-to-text and is sent as a message. When the agent has
	// answered, the answer is read aloud and listening starts again. No
	// wake word: the mode is opened and closed by hand.
	import { onDestroy, onMount } from 'svelte';
	import { fade } from 'svelte/transition';
	import Icon from '#lib/components/Icon.svelte';
	import VoiceOrb from './VoiceOrb.svelte';
	import { apiFetch, responseError } from '#lib/api.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { speak, speech, speakable, stopSpeaking } from '#lib/agent-speech.svelte.ts';
	import { toWav16k } from '#lib/speech-audio.ts';
	import type { Chat } from '#lib/agent.ts';

	let {
		chat,
		busy,
		voiceNas = false,
		calm = false,
		onsend,
		onclose,
	}: {
		chat: Chat | null;
		busy: boolean;
		/** Speech to text of the NAS itself (WAV) instead of the Whisper address. */
		voiceNas?: boolean;
		calm?: boolean;
		onsend: (text: string) => void | boolean | Promise<void | boolean>;
		onclose: () => void;
	} = $props();

	type Phase =
		'starting' | 'listening' | 'hearing' | 'reading' | 'thinking' | 'speaking' | 'paused';
	let phase = $state<Phase>('starting');
	let heard = $state('');
	let answer = $state('');
	let problem = $state('');
	let level = $state(0);

	let stream: MediaStream | null = null;
	let ctx: AudioContext | null = null;
	let recorder: MediaRecorder | null = null;
	let chunks: Blob[] = [];
	let frame = 0;
	let closed = false;
	// What the answer to the last question must come after.
	let asked = 0;

	// Loudness: the room's own noise is followed slowly; speech is what
	// stands clearly above it.
	const START = 0.035;
	const QUIET_MS = 1300;
	const MIN_MS = 350;
	const MAX_MS = 30000;
	let noise = 0.01;
	let began = 0;
	let lastLoud = 0;
	// Talking over the answer (0.95): clearly louder than what the
	// loudspeaker leaves in the microphone, for a quarter of a second.
	const OVER = 0.09;
	const OVER_MS = 260;
	let overSince = 0;

	// The wake word (0.95), kept on this device: empty is off. With it, only
	// a sentence that begins with the word is sent; the word alone, or an
	// answer just given, leaves a few seconds to speak without it.
	const WAKE_KEY = 'titanos.agent.talk.wake';
	const FREE_MS = 9000;
	function stored(): string {
		try {
			return localStorage.getItem(WAKE_KEY) ?? '';
		} catch {
			return '';
		}
	}
	let wake = $state(stored());
	let wakeDraft = $state(stored() || 'Titan');
	let freeUntil = 0;
	function setWake(on: boolean) {
		wake = on ? wakeDraft.trim() || 'Titan' : '';
		wakeDraft = wake || wakeDraft;
		try {
			if (wake) localStorage.setItem(WAKE_KEY, wake);
			else localStorage.removeItem(WAKE_KEY);
		} catch {
			// Kept for this visit only.
		}
	}
	const plain = (text: string) =>
		text
			.toLowerCase()
			.normalize('NFD')
			.replace(/[^a-z0-9 ]/g, '')
			.trim();
	// One letter may be off: speech to text writes names its own way.
	function near(a: string, b: string): boolean {
		if (a === b) return true;
		if (Math.abs(a.length - b.length) > 1 || b.length < 4) return false;
		let i = 0;
		let j = 0;
		let off = 0;
		while (i < a.length && j < b.length) {
			if (a[i] === b[j]) {
				i++;
				j++;
			} else {
				if (++off > 1) return false;
				if (a.length > b.length) i++;
				else if (b.length > a.length) j++;
				else {
					i++;
					j++;
				}
			}
		}
		return off + (a.length - i) + (b.length - j) <= 1;
	}
	/** What to send of what was said: all of it, the part after the wake
	 *  word, or nothing ('' with waiting = the word alone was said). */
	function afterWake(said: string): { text: string; waiting?: boolean } {
		if (!wake) return { text: said };
		const want = plain(wake).split(' ').filter(Boolean);
		const words = said.split(/\s+/);
		for (let at = 0; at < Math.min(3, words.length); at++) {
			const got = words.slice(at, at + want.length).map(plain);
			if (got.length === want.length && got.every((w, n) => near(w, want[n]))) {
				const rest = words
					.slice(at + want.length)
					.join(' ')
					.replace(/^[\s,.:;!?-]+/, '');
				return rest ? { text: rest } : { text: '', waiting: true };
			}
		}
		return performance.now() < freeUntil ? { text: said } : { text: '' };
	}

	function startRecording() {
		if (!stream) return;
		const type = ['audio/webm', 'audio/mp4', 'audio/ogg'].find(
			(m) => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported?.(m),
		);
		recorder = new MediaRecorder(stream, type ? { mimeType: type } : undefined);
		chunks = [];
		recorder.ondataavailable = (e) => e.data.size && chunks.push(e.data);
		const mine = recorder;
		recorder.onstop = () => {
			const long = performance.now() - began >= MIN_MS;
			const blob = new Blob(chunks, { type: mine.mimeType || type || 'audio/webm' });
			if (closed || phase !== 'reading') return;
			if (!long || !blob.size) {
				phase = 'listening';
				return;
			}
			void understand(blob);
		};
		recorder.start();
		began = lastLoud = performance.now();
		phase = 'hearing';
	}

	function tick() {
		frame = requestAnimationFrame(tick);
		if (!analyser) return;
		analyser.getFloatTimeDomainData(samples);
		let sum = 0;
		for (const s of samples) sum += s * s;
		const rms = Math.sqrt(sum / samples.length);
		level = Math.min(1, rms * 9);
		const now = performance.now();
		if (phase === 'listening') {
			noise = noise * 0.97 + rms * 0.03;
			if (rms > Math.max(START, noise * 3)) startRecording();
		} else if (phase === 'hearing') {
			if (rms > Math.max(START * 0.6, noise * 2)) lastLoud = now;
			if (now - lastLoud > QUIET_MS || now - began > MAX_MS) {
				phase = 'reading';
				recorder?.stop();
			}
		} else if (phase === 'speaking') {
			// The person talks over the answer: stop reading and listen.
			if (rms > Math.max(OVER, noise * 5)) {
				overSince ||= now;
				if (now - overSince > OVER_MS) {
					overSince = 0;
					stopSpeaking();
					freeUntil = now + FREE_MS;
					startRecording();
				}
			} else overSince = 0;
		}
	}
	let analyser: AnalyserNode | null = null;
	let samples = new Float32Array(1024);
	// What the figure draws while listening: the microphone's spectrum.
	let bands = new Uint8Array(512);
	function spectrum(): Uint8Array | null {
		if (!analyser || (phase !== 'listening' && phase !== 'hearing')) return null;
		analyser.getByteFrequencyData(bands);
		return bands;
	}

	async function understand(blob: Blob) {
		try {
			const res = voiceNas
				? await apiFetch(`/agent/speech/hear?lang=${i18n.locale}`, {
						method: 'POST',
						headers: { 'Content-Type': 'audio/wav' },
						body: await toWav16k(blob),
					})
				: await apiFetch(`/agent/transcribe?lang=${i18n.locale}`, {
						method: 'POST',
						headers: { 'Content-Type': blob.type },
						body: blob,
					});
			if (!res.ok) throw await responseError(res);
			const said = ((await res.json()) as { text: string }).text?.trim() ?? '';
			if (closed) return;
			// Nothing, or only a noise the model wrote down as a word or two
			// of filler: keep listening.
			if (said.replace(/[^\p{L}\p{N}]/gu, '').length < 2) {
				phase = 'listening';
				return;
			}
			const { text, waiting } = afterWake(said);
			if (!text) {
				// Not meant for the agent, or only its name: listen on.
				if (waiting) freeUntil = performance.now() + FREE_MS;
				phase = 'listening';
				return;
			}
			heard = text;
			answer = '';
			problem = '';
			asked = chat?.entries.length ?? 0;
			phase = 'thinking';
			// Nothing sent (the NAS or the model does not answer): listen
			// again instead of thinking for ever (0.99).
			if ((await onsend(text)) === false && !closed) {
				problem = t('agent.talk.notSent');
				phase = 'listening';
			}
		} catch (e) {
			if (closed) return;
			problem = apiErrorText(e);
			phase = 'listening';
		}
	}

	// The run ended: read the answer, or stop where the person must decide.
	let wasBusy = false;
	$effect(() => {
		const ended = wasBusy && !busy;
		wasBusy = busy;
		if (!ended || phase !== 'thinking') return;
		if (chat?.pending) {
			problem = t('agent.talk.pending');
			phase = 'paused';
			return;
		}
		const last = chat?.entries.slice(asked).findLast((e) => e.role === 'assistant' && e.content);
		if (!last?.content) {
			phase = 'listening';
			return;
		}
		answer = speakable(last.content);
		phase = 'speaking';
		speak('talk-' + last.id, last.content, i18n.locale);
		// Nothing to say it with, or it did not start: go on listening.
		if (!speech.id) phase = 'listening';
	});
	$effect(() => {
		if (phase === 'speaking' && !speech.id) {
			// A follow-up right after the answer needs no wake word.
			freeUntil = performance.now() + FREE_MS;
			phase = 'listening';
		}
	});

	function tap() {
		if (phase === 'speaking') {
			stopSpeaking();
			phase = 'listening';
		} else if (phase === 'paused') {
			problem = '';
			phase = 'listening';
		} else if (phase === 'listening') {
			phase = 'paused';
		} else if (phase === 'hearing') {
			phase = 'reading';
			recorder?.stop();
		}
	}

	onMount(async () => {
		try {
			stream = await navigator.mediaDevices.getUserMedia({
				audio: { echoCancellation: true, noiseSuppression: true },
			});
			if (closed) {
				stream.getTracks().forEach((track) => track.stop());
				return;
			}
			const Ctx =
				window.AudioContext ??
				(window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
			ctx = new Ctx();
			analyser = ctx.createAnalyser();
			analyser.fftSize = 1024;
			samples = new Float32Array(analyser.fftSize);
			bands = new Uint8Array(analyser.frequencyBinCount);
			analyser.smoothingTimeConstant = 0.75;
			ctx.createMediaStreamSource(stream).connect(analyser);
			phase = 'listening';
			tick();
		} catch {
			problem = t('agent.voiceNoMic');
			phase = 'paused';
		}
	});
	onDestroy(() => {
		closed = true;
		cancelAnimationFrame(frame);
		if (recorder?.state === 'recording') recorder.stop();
		stream?.getTracks().forEach((track) => track.stop());
		void ctx?.close();
		stopSpeaking();
	});

	const label = $derived(t(`agent.talk.${phase}` as MessageKey));
	const tapLabel = $derived(t(`agent.talk.tap.${phase}` as MessageKey));
	const figure = $derived(
		phase === 'thinking' || phase === 'reading' || phase === 'starting'
			? 'think'
			: phase === 'speaking'
				? 'speak'
				: phase === 'paused'
					? 'rest'
					: 'listen',
	);
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div
	class="talk"
	class:calm
	role="dialog"
	aria-modal="true"
	aria-label={t('agent.talk.title')}
	transition:fade={{ duration: calm ? 0 : 220 }}
>
	<button type="button" class="end" onclick={onclose} aria-label={t('agent.talk.end')}>
		<Icon name="close" size={20} />
	</button>
	<div class="stage">
		<button
			type="button"
			class="orb"
			data-figure={figure}
			onclick={tap}
			aria-label={tapLabel}
			title={tapLabel}
		>
			<VoiceOrb {figure} {level} {spectrum} {calm} />
		</button>
		<p class="state" aria-live="polite">{label}</p>
		{#if problem}<p class="problem" role="alert">{problem}</p>{/if}
	</div>
	<div class="said">
		{#if heard}<p class="heard">{heard}</p>{/if}
		{#if answer}<p class="answer">{answer}</p>{/if}
		{#if !heard && !problem}
			<p class="hint">{wake ? t('agent.talk.hintWake', { word: wake }) : t('agent.talk.hint')}</p>
		{/if}
	</div>
	<div class="wake">
		<label class="wake-on">
			<input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!wake}
				onchange={(e) => setWake(e.currentTarget.checked)}
			/>
			<span>{t('agent.talk.wake')}</span>
		</label>
		<input
			class="field wake-word"
			aria-label={t('agent.talk.wakeWord')}
			bind:value={wakeDraft}
			maxlength="24"
			onchange={() => wake && setWake(true)}
		/>
	</div>
	<button type="button" class="done" onclick={onclose}>{t('agent.talk.end')}</button>
</div>

<style>
	.talk {
		position: fixed;
		inset: 0;
		z-index: 60;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-4);
		padding: calc(env(safe-area-inset-top) + var(--space-5)) var(--space-5)
			calc(env(safe-area-inset-bottom) + var(--space-5));
		background: var(--bg);
		color: var(--text);
		/* The agent's own orange (its page sets it on .agent; this layer
		   lies outside of that). */
		--ag-warm: #d9734a;
	}
	.end {
		align-self: flex-end;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: 0;
		border-radius: 50%;
		background: var(--surface-2, var(--surface));
		color: inherit;
		cursor: pointer;
	}
	.stage {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-4);
	}
	.orb {
		position: relative;
		display: grid;
		place-items: center;
		width: min(78vw, 46vh, 320px);
		aspect-ratio: 1;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: none;
		cursor: pointer;
		-webkit-tap-highlight-color: transparent;
	}
	.state {
		margin: 0;
		font-size: 1.15em;
		font-weight: 600;
	}
	.problem {
		margin: 0;
		max-width: 34ch;
		text-align: center;
		color: var(--status-bad, var(--danger));
		overflow-wrap: anywhere;
	}
	.said {
		width: min(100%, 560px);
		max-height: 34vh;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		text-align: center;
	}
	.said p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.heard,
	.hint {
		color: var(--muted);
	}
	.wake {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		color: var(--muted);
	}
	.wake-on {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
	}
	.wake-word {
		width: 9.5em;
		min-height: 40px;
		text-align: center;
	}
	.done {
		min-height: 48px;
		padding: 0 32px;
		border: 0;
		border-radius: 999px;
		background: var(--surface-2, var(--surface));
		color: inherit;
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
</style>
