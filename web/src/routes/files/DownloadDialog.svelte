<script lang="ts">
 import {api,ApiError,type FileRoot} from '#lib/api.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 import {t,errorText} from '#lib/i18n/index.svelte.ts';
 import {formatBytes} from '#lib/format.ts';
 let {root,paths,onclose}:{root:FileRoot;paths:string[];onclose:()=>void}=$props();
 let name=$state(''),data=$state<{files:number;bytes:number}|null>(null),error=$state('');
 $effect(()=>{name=(paths.length===1?paths[0].split('/').at(-1):root.name)+'.zip';const c=new AbortController();void api.post<{files:number;bytes:number}>('/files/inspect',{root:root.id,paths},c.signal).then(r=>{if(!c.signal.aborted)data=r;}).catch(e=>{if(!c.signal.aborted)error=errorText(e instanceof ApiError?e.code:'internal');});return()=>c.abort();});
 const valid=$derived(name.length<=200 && name.length>4 && /\.zip$/i.test(name) && !/[\\/\r\n]/.test(name));
 function download(){if(!data||!valid)return;const q=new URLSearchParams({root:root.id,name});for(const p of paths)q.append('path',p);location.href='/api/v1/files/zip?'+q;onclose();}
</script>
<Dialog title={t('files.downloadArchive')} {onclose}>
 <label class="field-row"><span>{t('files.archiveName')}</span><input class="field" bind:value={name}/></label>
 {#if data}<p>{t('files.downloadCount',{count:data.files})} · {formatBytes(data.bytes)}</p>{:else if !error}<p role="status">{t('files.searchRunning')}</p>{/if}
 <p class="muted">{t('files.downloadHint')}</p>
 {#if error}<p role="alert" class="notice-error">{error}</p>{/if}
 <div class="dialog-actions"><button class="btn" onclick={onclose}>{t('common.cancel')}</button><button class="btn btn-primary" disabled={!data||!valid} onclick={download}>{t('files.download')}</button></div>
</Dialog>
