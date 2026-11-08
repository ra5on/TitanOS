// PLAN 72: in the agent's window on a computer the views of the functions
// (Papierkram, Fristen, Hardware-Berater, …) are pages in the main area
// next to the side bar, not panels over it. The agent page sets this
// context; the sheets then render as a page. On the phone they stay full
// screen sheets.
import { getContext, setContext } from 'svelte';

const KEY = Symbol('agent-feature-page');

export function setFeaturePage(on: boolean) {
	setContext(KEY, on);
}

export function featurePage(): boolean {
	return getContext<boolean | undefined>(KEY) ?? false;
}
