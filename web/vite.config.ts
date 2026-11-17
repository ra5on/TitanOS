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
/** How many release notes go along with the manual. */
const RELEASE_NOTES = 12;

/** The file names vX.Y.Z.md of the newest versions in dir, newest first. */
function newestReleases(dir: string, count: number): string[] {
	const num = (name: string) =>
		name
			.replace(/^v/, '')
			.replace(/\.md$/, '')
			.split('.')
			.map((p) => Number(p));
	return (readdirSync(dir) as string[])
		.filter((name) => /^v\d+\.\d+\.\d+\.md$/.test(name))
		.sort((a, b) => {
			const x = num(a);
			const y = num(b);
			for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return y[i] - x[i];
			return 0;
		})
		.slice(0, count);
}

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
			// 1.10: the notes of the newest releases, for "Was ist neu?" of
			// the agent (tool release_notes). Not all 160: they would fill
			// titand with history nobody asks for.
			mkdirSync(`${target}/releases`, { recursive: true });
			for (const name of newestReleases('../docs/releases', RELEASE_NOTES)) {
				cpSync(`../docs/releases/${name}`, `${target}/releases/${name}`);
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
			// hash of its own start script to the policy in index.html. Everything
			// else stays on this NAS, except https pictures (app icons): after an
			// injection nothing can be loaded from or sent to elsewhere.
			csp: {
				directives: {
					'default-src': ['self'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': ['self', 'data:', 'blob:', 'https:'],
					'media-src': ['self', 'blob:'],
					'font-src': ['self', 'data:'],
					'connect-src': ['self'],
					'worker-src': ['self', 'blob:'],
					// PLAN 75-14: apps with their own desktop stream are framed under
					// their HTTPS names; titand's header narrows this to exactly those
					// names (api/ui.go narrowFrames), both policies hold.
					'frame-src': ['self', 'https:'],
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
