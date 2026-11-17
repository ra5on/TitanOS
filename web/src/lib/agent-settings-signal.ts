// The settings of the KI-Agent (Einstellungen › KI-Agent) reach an agent
// page that is open elsewhere – a window of the desktop (its own document),
// another tab – at once (owner 17 Nov 2026: "bei Denken hoch auswähle …
// wird das nicht direkt übernommen. Ich müsste TitanOS neu starten"). The
// page then reads its state again, so a chat without a choice of its own
// shows the new default model and thinking level.
const CHANNEL = 'titanos-agent-settings';

/** Tells every open agent page that the settings were saved. */
export function announceAgentSettings(): void {
	if (typeof BroadcastChannel === 'undefined') return;
	const channel = new BroadcastChannel(CHANNEL);
	channel.postMessage('saved');
	channel.close();
}

/** Calls changed whenever the settings were saved elsewhere; returns the
 *  function that stops listening. */
export function onAgentSettings(changed: () => void): () => void {
	if (typeof BroadcastChannel === 'undefined') return () => {};
	const channel = new BroadcastChannel(CHANNEL);
	channel.onmessage = () => changed();
	return () => channel.close();
}
