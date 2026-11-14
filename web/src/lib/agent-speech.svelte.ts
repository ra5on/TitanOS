// Reading answers of the KI-Agent aloud (PLAN 51) with the speech of the
// device itself (Web Speech API): no server, no model. iOS and Android use
// their own voices; without them the button is not shown.

export const speech = $state<{ id: string }>({ id: '' });

// PLAN 92: with the speech services of the NAS switched on for the agent,
// answers are read by the voice chosen there (POST /agent/speech/say)
// instead of the device's own; `nas` says so (set by the agent page).
export const nasSpeech = $state<{ say: boolean; hear: boolean }>({ say: false, hear: false });
let playing: HTMLAudioElement | null = null;
let asked: AbortController | null = null;

export function canSpeak(): boolean {
	return nasSpeech.say || (typeof window !== 'undefined' && 'speechSynthesis' in window);
}

function stopNas() {
	asked?.abort();
	asked = null;
	if (playing) {
		playing.pause();
		URL.revokeObjectURL(playing.src);
		playing = null;
	}
}

async function speakNas(id: string, text: string) {
	const { apiFetch } = await import('./api.ts');
	const control = new AbortController();
	asked = control;
	speech.id = id;
	try {
		const res = await apiFetch('/agent/speech/say', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			// The services speak at most 4000 characters in one go.
			body: JSON.stringify({ text: text.slice(0, 4000) }),
			signal: control.signal,
		});
		if (!res.ok) throw new Error(String(res.status));
		const audio = new Audio(URL.createObjectURL(await res.blob()));
		playing = audio;
		audio.onended = audio.onerror = () => {
			if (speech.id === id) speech.id = '';
			if (playing === audio) stopNas();
		};
		await audio.play();
	} catch {
		if (speech.id === id) speech.id = '';
	}
}

/** Plain text from the Markdown of an answer: no tables, code or links. */
export function speakable(markdown: string): string {
	return markdown
		.replace(/```[\s\S]*?```/g, ' ')
		.replace(/^\s*\|.*\|\s*$/gm, ' ')
		.replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
		.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
		.replace(/[*_`#>~]+/g, '')
		.replace(/^\s*[-+]\s+/gm, '')
		.replace(/\s+/g, ' ')
		.trim();
}

/** Reads a text aloud; the same id again stops it. */
/** Speaking rate of the account (PLAN 53), set by the agent page. */
export const speechPrefs = { rate: 1 };

export function speak(
	id: string,
	markdown: string,
	locale: 'de' | 'en',
	rate = speechPrefs.rate,
): void {
	if (!canSpeak()) return;
	const again = speech.id === id;
	stopSpeaking();
	if (again) return;
	const text = speakable(markdown);
	if (!text) return;
	if (nasSpeech.say) {
		void speakNas(id, text);
		return;
	}
	const synth = window.speechSynthesis;
	const utterance = new SpeechSynthesisUtterance(text);
	utterance.lang = locale === 'de' ? 'de-DE' : 'en-GB';
	const voice = synth.getVoices().find((v) => v.lang.toLowerCase().startsWith(locale));
	if (voice) utterance.voice = voice;
	utterance.rate = Math.min(2, Math.max(0.5, rate));
	utterance.onend = utterance.onerror = () => {
		if (speech.id === id) speech.id = '';
	};
	speech.id = id;
	synth.speak(utterance);
}

export function stopSpeaking(): void {
	stopNas();
	if (typeof window !== 'undefined' && 'speechSynthesis' in window) window.speechSynthesis.cancel();
	speech.id = '';
}
