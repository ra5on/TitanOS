<script lang="ts">
 import Dialog from '#lib/components/Dialog.svelte';
 import { t } from '#lib/i18n/index.svelte.ts';
 import { previewKind } from '#lib/file-actions.ts';
 let {root,path,size,onclose,onprevious,onnext}: {root:string;path:string;size:number;onclose:()=>void;onprevious?:()=>void;onnext?:()=>void}=$props();
 let text=$state(''), url=$state(''), failed=$state(false), loading=$state(true);
 const stream=$derived('/api/v1/files/download?'+new URLSearchParams({root,path,media:'1'}));
 function keyboard(event:KeyboardEvent){if(kind!=='image'||event.altKey||event.ctrlKey||event.metaKey)return;if(event.key==='ArrowLeft'&&onprevious){event.preventDefault();onprevious();}if(event.key==='ArrowRight'&&onnext){event.preventDefault();onnext();}}
 const kind=$derived(previewKind(path,size));
 $effect(()=>{
  loading=true;failed=false;url='';text='';
  const controller=new AbortController();let objectURL='';
  const limit=(kind==='image'||kind==='pdf')?20*1024*1024:512*1024;
  async function read(){
   try {
    if(kind==='audio'||kind==='video'){loading=false;return;}
    if(!kind)throw new Error('unsupported');
    const res=await fetch('/api/v1/files/download?'+new URLSearchParams({root,path}),{credentials:'same-origin',signal:controller.signal});
    if(!res.ok || Number(res.headers.get('content-length'))>limit || !res.body)throw new Error('unavailable');
    const reader=res.body.getReader(), chunks:Uint8Array[]=[];let length=0;
    for(;;){const {done,value}=await reader.read();if(done)break;length+=value.byteLength;if(length>limit){await reader.cancel();throw new Error('too large');}chunks.push(value);}
    const bytes=new Uint8Array(length);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}
    if(controller.signal.aborted)return;
    if(kind==='text')text=new TextDecoder('utf-8',{fatal:true}).decode(bytes);
    else {const ext=path.split('.').pop()!.toLowerCase();objectURL=URL.createObjectURL(new Blob([bytes],{type:kind==='pdf'?'application/pdf':'image/'+(ext==='jpg'?'jpeg':ext)}));url=objectURL;}
   }catch {if(!controller.signal.aborted)failed=true;}
   finally {if(!controller.signal.aborted)loading=false;}
  }
  read();return()=>{controller.abort();if(objectURL)URL.revokeObjectURL(objectURL);};
 });
</script>
<svelte:window onkeydown={keyboard}/>
<Dialog title={t('files.previewTitle',{name:path})} {onclose} wide>
 {#if loading}<p role="status">{t('files.previewLoading')}</p>{:else if failed}<p class="notice-error" role="alert">{t('files.previewFailed')}</p>{:else if kind==='image'}<img src={url} alt={path} onerror={()=>{failed=true;}} />{:else if kind==='audio'}<audio controls preload="metadata" src={stream} onerror={()=>failed=true}></audio><p class="muted">{t('files.mediaHint')}</p>{:else if kind==='video'}<!-- Native media controls provide keyboard access; no invented subtitle track. -->
 <!-- svelte-ignore a11y_media_has_caption -->
 <video controls preload="metadata" src={stream} onerror={()=>failed=true}></video><p class="muted">{t('files.mediaHint')}</p>{:else if kind==='pdf'}<object data={url} type="application/pdf" aria-label={path}><p>{t('files.pdfFallback')}</p></object>{:else}<pre>{text}</pre>{/if}
 {#if kind==='image'}<div class="gallery"><button class="btn" disabled={!onprevious} onclick={onprevious}>{t('files.previousImage')}</button><button class="btn" disabled={!onnext} onclick={onnext}>{t('files.nextImage')}</button></div>{/if}
 <a class="btn" href={'/api/v1/files/download?'+new URLSearchParams({root,path})}>{t('files.download')}</a>
 <div class="dialog-actions"><button type="button" class="btn" onclick={onclose}>{t('files.editClose')}</button></div>
</Dialog>
<style>video{width:100%;max-height:65vh;}audio{width:100%;}object{display:block;width:100%;height:65vh;border:0;}.gallery{display:flex;gap:8px;margin:12px 0;}img{max-width:100%;max-height:65vh;object-fit:contain;}pre{max-height:65vh;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;font-family:var(--font-mono,monospace);font-size:0.875rem;}</style>
