import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vitest/config';

const here = (path: string) => new URL(path, import.meta.url).pathname;

// Component tests (tests/components) and unit tests of lib modules that need
// runes or the DOM (tests/lib) run in jsdom with the plain Svelte plugin; the
// node:test suite (tests/*.test.mjs) stays as it is.
export default defineConfig({
	plugins: [svelte()],
	resolve: {
		// The browser build of Svelte: components mount, effects run.
		conditions: ['browser'],
		alias: [
			{ find: /^#lib\/(.*)$/, replacement: here('./src/lib/') + '$1' },
			{ find: '$app/navigation', replacement: here('./tests/components/stubs/app-navigation.ts') },
			{ find: '$app/state', replacement: here('./tests/components/stubs/app-state.ts') },
		],
	},
	test: {
		environment: 'jsdom',
		include: ['tests/components/**/*.test.ts', 'tests/lib/**/*.test.ts'],
		setupFiles: ['tests/components/setup.ts'],
	},
});
