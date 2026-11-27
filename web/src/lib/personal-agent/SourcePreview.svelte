<script lang="ts">
	import { onMount } from 'svelte';
	import { apiErrorText } from '#lib/i18n/index.svelte.ts';
	import { msg, sourceTitle } from './labels.ts';
	import type { Source } from './api.ts';
	import { sourceLocation } from './sources.ts';
	import Sheet from './Sheet.svelte';
	let { source, onclose, calm }: { source: Source; onclose: () => void; calm: boolean } = $props();
	let url = $state(''),
		error = $state(''),
		page = $state(1);
	onMount(() => {
		const controller = new AbortController();
		let objectURL = '';
		async function open() {
			const dest = sourceLocation(source);
			if (!dest?.pdf) return;
			try {
				const r = await fetch(dest.href, { credentials: 'same-origin', signal: controller.signal });
				if (!r.ok) throw Error(msg('previewFail'));
				const limit = 20 * 1024 * 1024;
				if (!r.body || Number(r.headers.get('content-length')) > limit)
					throw Error(msg('tooLarge'));
				const reader = r.body.getReader(),
					parts: Uint8Array<ArrayBuffer>[] = [];
				let total = 0;
				try {
					for (;;) {
						const { done, value } = await reader.read();
						if (done) break;
						total += value.length;
						if (total > limit) {
							void reader.cancel();
							throw Error(msg('tooLarge'));
						}
						parts.push(new Uint8Array(value));
					}
				} finally {
					reader.releaseLock();
				}
				if (controller.signal.aborted) return;
				objectURL = URL.createObjectURL(new Blob(parts, { type: 'application/pdf' }));
				page = dest.page;
				url = objectURL + '#page=' + page;
			} catch (e) {
				if (!controller.signal.aborted) error = apiErrorText(e);
			}
		}
		void open();
		return () => {
			controller.abort();
			if (objectURL) URL.revokeObjectURL(objectURL);
		};
	});
</script>

<Sheet title={sourceTitle(source)} {onclose} {calm}>
	{#if error}<p class="pa-warning" role="alert">{error}</p>
	{:else if url}<small>{msg('page')} {page}</small><iframe
			class="pdf"
			src={url}
			title={sourceTitle(source)}
		></iframe><a href={url} target="_blank" rel="noreferrer noopener">{msg('openSource')}</a>
	{:else}<p role="status">{msg('loading')}</p>{/if}
</Sheet>

<style>
	.pdf {
		width: 100%;
		height: 52vh;
		max-height: 520px;
		border: 0;
		background: white;
		border-radius: 8px;
	}
</style>
