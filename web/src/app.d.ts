// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {
		interface PageState {
			/** PLAN 63: the Docker app shown full screen in its remote view on the phone. */
			remote?: string;
			/** PLAN 66: a web app shown full screen on the phone, with the file it opened. */
			webapp?: { id: string; root?: string; path?: string };
		}
	}
}

export {};
