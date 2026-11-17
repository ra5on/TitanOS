// 1.03: a save of the agent's functions that another tab or administrator
// overtook (409 agent_features_changed) is not lost quietly. What this
// save changed is put on the new state when nobody else changed the same
// function in the meantime; otherwise the page shows the new state and
// says the change was not saved.

type Settings = Record<string, unknown>;

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/**
 * `base`: the state the change was made on, `mine`: the change, `theirs`:
 * the state now stored. Returns what to save (with the new `rev`), `theirs`
 * itself when the change touched nothing, or null when the same function
 * was changed elsewhere too.
 */
export function rebaseFeatures<T extends Settings>(base: T, mine: T, theirs: T): T | null {
	const changed = Object.keys(mine).filter((key) => key !== 'rev' && !same(mine[key], base[key]));
	if (changed.some((key) => !same(theirs[key], base[key]))) return null;
	if (!changed.length) return theirs;
	const out: Settings = { ...theirs };
	for (const key of changed) out[key] = mine[key];
	return out as T;
}
