// Reading answers of the KI-Agent aloud (PLAN 51) with the speech of the
// device itself (Web Speech API): no server, no model. iOS and Android use
// their own voices; without them the button is not shown.

export const speech = $state<{ id: string }>({ id: '' });

export function canSpeak(): boolean {
	return typeof window !== 'undefined' && 'speechSynthesis' in window;
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
export function speak(id: string, markdown: string, locale: 'de' | 'en'): void {
	if (!canSpeak()) return;
	const synth = window.speechSynthesis;
	const again = speech.id === id;
	synth.cancel();
	speech.id = '';
	if (again) return;
	const text = speakable(markdown);
	if (!text) return;
	const utterance = new SpeechSynthesisUtterance(text);
	utterance.lang = locale === 'de' ? 'de-DE' : 'en-GB';
	const voice = synth.getVoices().find((v) => v.lang.toLowerCase().startsWith(locale));
	if (voice) utterance.voice = voice;
	utterance.onend = utterance.onerror = () => {
		if (speech.id === id) speech.id = '';
	};
	speech.id = id;
	synth.speak(utterance);
}

export function stopSpeaking(): void {
	if (canSpeak()) window.speechSynthesis.cancel();
	speech.id = '';
}
