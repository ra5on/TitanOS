<script lang="ts">
 import Icon from '#lib/components/Icon.svelte';
 let { root, path, size }: { root: string; path: string; size: number } = $props();
 let host = $state<HTMLSpanElement>();
 let url = $state('');
 let failed = $state(false);
 // Avoid full-size downloads of large originals while browsing a folder.
 const limit = 2 * 1024 * 1024;
 $effect(() => {
  if (!host || size > limit) return;
  const controller = new AbortController();
  let objectURL = '';
  async function load() {
   try {
    const res = await fetch('/api/v1/files/download?' + new URLSearchParams({ root, path }), { signal: controller.signal, credentials: 'same-origin' });
    if (!res.ok || Number(res.headers.get('content-length')) > limit || !res.body) throw new Error('unavailable');
    const reader = res.body.getReader(), chunks: Uint8Array[] = [];
    let length = 0;
    for (;;) {
     const { done, value } = await reader.read();
     if (done) break;
     length += value.byteLength;
     if (length > limit) { await reader.cancel(); throw new Error('too large'); }
     chunks.push(value);
    }
    if (controller.signal.aborted) return;
    const bytes = new Uint8Array(length); let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
    const ext = path.split('.').pop()!.toLowerCase();
    objectURL = URL.createObjectURL(new Blob([bytes], { type: 'image/' + (ext === 'jpg' ? 'jpeg' : ext) }));
    url = objectURL;
   } catch { failed = true; controller.abort(); }
  }
  const observer = new IntersectionObserver(entries => {
   if (entries.some(entry => entry.isIntersecting)) { observer.disconnect(); void load(); }
  });
  observer.observe(host);
  return () => { observer.disconnect(); controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
 });
</script>
<span class="thumbnail" bind:this={host} aria-hidden="true">
 {#if url && !failed}<img src={url} alt="" onerror={() => { failed = true; }} />{:else}<Icon name="file" size={48} />{/if}
</span>
<style>
 .thumbnail { display:flex; align-items:center; justify-content:center; width:100%; height:96px; }
 img { max-width:100%; max-height:96px; object-fit:contain; border-radius:var(--radius-small); }
</style>
