import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

// The build output goes straight into the Go package that embeds it into titand.
const out = '../backend/internal/webui/dist';

export default defineConfig(({ mode }) => ({
	plugins: [
		sveltekit({
			// Single-page app: titand answers every unknown path with index.html.
			adapter: adapter({ pages: out, assets: out, fallback: 'index.html' }),
			// Only scripts that are part of the build may run. SvelteKit adds the
			// hash of its own start script to the policy in index.html.
			csp: {
				directives: {
					'script-src': ['self'],
					'object-src': ['none'],
					'base-uri': ['self']
				}
			}
		})
	],
	server: {
		// During development the API comes from a locally running titand.
		proxy: {
			'/api': { target: loadEnv(mode, '.', '').TITANOS_DEV_API || 'http://127.0.0.1:8080', ws: true }
		}
	}
}));
