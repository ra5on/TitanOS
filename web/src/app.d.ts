// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {}
}

// The attributes gridstack reads a tile's place in the grid from.
declare module 'svelte/elements' {
	interface HTMLAttributes<T> {
		'gs-id'?: string;
		'gs-x'?: number;
		'gs-y'?: number;
		'gs-w'?: number;
		'gs-h'?: number;
		'gs-min-w'?: number;
		'gs-min-h'?: number;
		'gs-auto-position'?: 'true';
	}
}

export {};
