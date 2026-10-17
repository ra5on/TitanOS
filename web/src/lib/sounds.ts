/** Original soft sine tones, generated locally; no downloads or audio samples. */
export const tones = ['bell', 'wood', 'soft', 'chime', 'double'] as const;
export type Tone = typeof tones[number] | 'none';
export type SoundSettings = { tone: Tone; volume: number; severity: 'all' | 'important' };
export function sanitizeSound(value: unknown): SoundSettings {
 const raw = (value && typeof value === 'object' ? value : {}) as Partial<SoundSettings>;
 return { tone: tones.includes(raw.tone as typeof tones[number]) ? raw.tone! : 'none', volume: Number.isFinite(raw.volume) ? Math.max(0, Math.min(100, raw.volume!)) : 35, severity: raw.severity === 'all' ? 'all' : 'important' };
}
export const notes: Record<Exclude<Tone, 'none'>, [number, number, number][]> = {
 bell: [[523.25, 0, 0.8], [784, 0.03, 0.65]],
 wood: [[261.63, 0, 0.22], [392, 0.01, 0.15]],
 soft: [[349.23, 0, 0.65]],
 chime: [[392, 0, 0.6], [523.25, 0.18, 0.65], [659.25, 0.36, 0.7]],
 double: [[440, 0, 0.3], [523.25, 0.36, 0.45]]
};
/** First load is a silent baseline; bursts play at most once every ten seconds. */
export class SoundGate {
 private known: Set<number> | null = null;
 private last = -Infinity;
 accept(messages: {id: number; severity: string}[], settings: SoundSettings, now: number): boolean {
  const fresh = this.known ? messages.filter(m => !this.known!.has(m.id)) : [];
  this.known = new Set(messages.map(m => m.id));
  if (settings.tone === 'none' || settings.volume === 0 || now - this.last < 10_000 || !fresh.some(m => settings.severity === 'all' || m.severity === 'warning' || m.severity === 'error')) return false;
  this.last = now;
  return true;
 }
}
