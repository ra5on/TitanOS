import { notes, sanitizeSound, SoundGate, type SoundSettings } from './sounds.ts';
export const sound = $state<{ settings: SoundSettings; ready: boolean }>({
	settings: sanitizeSound(null),
	ready: false,
});
let context: AudioContext | null = null;
let gate = new SoundGate();
export function resetSound() {
	sound.settings = sanitizeSound(null);
	gate = new SoundGate();
	sound.ready = false;
	if (context) void context.close().catch(() => {});
	context = null;
}
/** Called only by a direct user gesture, as browsers require. */
export async function armSound() {
	if (sound.settings.tone === 'none') return false;
	try {
		context ??= new AudioContext();
		await context.resume();
		sound.ready = context.state === 'running';
	} catch {
		sound.ready = false;
	}
	return sound.ready;
}
export function playSound(settings: SoundSettings): boolean {
	if (!context || context.state !== 'running' || settings.tone === 'none') return false;
	const start = context.currentTime + 0.015;
	for (const [frequency, offset, duration] of notes[settings.tone]) {
		const oscillator = context.createOscillator();
		const gain = context.createGain();
		oscillator.type = 'sine';
		oscillator.frequency.value = frequency;
		const at = start + offset;
		gain.gain.setValueAtTime(0, at);
		gain.gain.linearRampToValueAtTime((0.065 * settings.volume) / 100, at + 0.025);
		gain.gain.exponentialRampToValueAtTime(0.0001, at + duration);
		oscillator.connect(gain);
		gain.connect(context.destination);
		oscillator.onended = () => {
			oscillator.disconnect();
			gain.disconnect();
		};
		oscillator.start(at);
		oscillator.stop(at + duration + 0.02);
	}
	return true;
}
export function soundForNotices(messages: { id: number; severity: string }[]) {
	if (gate.accept(messages, sound.settings, performance.now())) playSound(sound.settings);
}
