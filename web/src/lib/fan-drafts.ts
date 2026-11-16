// The fan settings form (FanControl.svelte) is asked again every few seconds.
// A fan whose draft someone changed keeps it until it is saved; every other
// fan follows what the NAS answers (Durchsicht 102: after the first save
// every later change sprang back with the next answer).

export type FanDrafts<C> = {
	/** What the form shows and saves, per fan id. */
	drafts: Record<string, C>;
	/** The NAS's setting each draft started from (JSON), per fan id. */
	base: Record<string, string>;
};

/** Merges an answer of the NAS; `fresh` (the answer to a save) replaces every draft. */
export function takeFans<C>(
	now: FanDrafts<C>,
	fans: { id: string; control: C }[],
	fresh = false,
): FanDrafts<C> & { edited: boolean } {
	const drafts: Record<string, C> = {};
	const base: Record<string, string> = {};
	let edited = false;
	for (const fan of fans) {
		const draft = now.drafts[fan.id];
		const from = now.base[fan.id];
		const changed =
			!fresh && draft !== undefined && from !== undefined && JSON.stringify(draft) !== from;
		if (changed) {
			drafts[fan.id] = draft;
			base[fan.id] = from;
			edited = true;
		} else {
			drafts[fan.id] = structuredClone(fan.control);
			base[fan.id] = JSON.stringify(fan.control);
		}
	}
	return { drafts, base, edited };
}
