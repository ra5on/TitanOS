<script lang="ts">
 import {beforeNavigate,goto} from '$app/navigation';import Dialog from '#lib/components/Dialog.svelte';import {t} from '#lib/i18n/index.svelte.ts';
 let {dirty}:{dirty:boolean}=$props();let destination=$state<URL|null>(null),bypass=false;
 beforeNavigate(navigation=>{if(!dirty||bypass||navigation.willUnload)return;navigation.cancel();destination=navigation.to?.url??null;});
 function unload(event:BeforeUnloadEvent){if(dirty){event.preventDefault();event.returnValue='';}}
 function discard(){const url=destination;destination=null;if(!url)return;bypass=true;goto(url.href).finally(()=>bypass=false);}
</script>
<svelte:window onbeforeunload={unload}/>
{#if destination}<Dialog title={t('ops.unsaved')} onclose={()=>destination=null}><p>{t('ops.unsavedHint')}</p><div class="dialog-actions"><button class="btn" onclick={()=>destination=null}>{t('common.cancel')}</button><button class="btn btn-warn" onclick={discard}>{t('ops.discard')}</button></div></Dialog>{/if}
