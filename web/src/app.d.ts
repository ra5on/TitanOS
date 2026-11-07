// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {
		interface PageState {
			/** PLAN 63: the Docker app shown full screen in its remote view on the phone. */
			remote?: string;
		}
	}
}

export {};
