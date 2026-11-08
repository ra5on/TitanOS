// PLAN 72: marks the words of a search excerpt that begin with a word of
// the query, the same rule as the full-text index (letters and digits, lower
// case, prefix). Returns plain text parts; nothing is rendered as HTML.

export type Part = { text: string; hit: boolean };

export function queryTerms(query: string): string[] {
	return (query.toLowerCase().match(/[\p{L}\p{N}]+/gu) ?? []).filter((w) => [...w].length >= 2);
}

export function highlight(text: string, query: string): Part[] {
	const terms = queryTerms(query);
	const parts: Part[] = [];
	const push = (piece: string, hit: boolean) => {
		if (!piece) return;
		const last = parts.at(-1);
		if (last && last.hit === hit) last.text += piece;
		else parts.push({ text: piece, hit });
	};
	for (const piece of text.split(/([\p{L}\p{N}]+)/u)) {
		const lower = piece.toLowerCase();
		push(
			piece,
			terms.length > 0 &&
				/^[\p{L}\p{N}]/u.test(piece) &&
				terms.some((term) => lower.startsWith(term)),
		);
	}
	return parts;
}
