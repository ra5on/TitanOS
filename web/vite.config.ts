import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
// @ts-expect-error -- the project has no Node types; Vite runs this file in Node.
import { cpSync, mkdirSync, readdirSync, rmSync } from 'node:fs';
import { defineConfig, loadEnv, type Plugin } from 'vite';

// The build output goes straight into the Go package that embeds it into titand.
const out = '../backend/internal/webui/dist';

// The manual of TitanOS (README and docs/*.md) goes along, so that the
// assistant in titand can search it (see backend/internal/assistant/manual.go).
// It is copied to static/manual (not in Git) before the build; SvelteKit then
// puts it next to the pages like any static file.
function manual(): Plugin {
	return {
		name: 'titanos-manual',
		apply: 'build',
		buildStart() {
			const target = 'static/manual';
			rmSync(target, { recursive: true, force: true });
			mkdirSync(`${target}/docs`, { recursive: true });
			cpSync('../README.md', `${target}/README.md`);
			for (const name of readdirSync('../docs')) {
				if (name.endsWith('.md')) cpSync(`../docs/${name}`, `${target}/docs/${name}`);
			}
		},
	};
}

export default defineConfig(({ mode }) => ({
	plugins: [
		manual(),
		sveltekit({
			// Single-page app: titand answers every unknown path with index.html.
			adapter: adapter({ pages: out, assets: out, fallback: 'index.html' }),
			// Only scripts that are part of the build may run. SvelteKit adds the
			// hash of its own start script to the policy in index.html.
			csp: {
				directives: {
					'script-src': ['self'],
					'object-src': ['none'],
					'base-uri': ['self'],
				},
			},
		}),
	],
	server: {
		// During development the API comes from a locally running titand.
		proxy: {
			'/api': {
				target: loadEnv(mode, '.', '').TITANOS_DEV_API || 'http://127.0.0.1:8080',
				ws: true,
			},
		},
	},
}));
