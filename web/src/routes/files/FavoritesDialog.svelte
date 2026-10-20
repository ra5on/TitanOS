<script lang="ts">
 import Dialog from '#lib/components/Dialog.svelte';
 import {t} from '#lib/i18n/index.svelte.ts';
 import type {Favorite} from '#lib/file-actions.ts';
 let {favorites,onsave,onclose}:{favorites:Favorite[];onsave:(next:Favorite[])=>void;onclose:()=>void}=$props();
 // svelte-ignore state_referenced_locally
 let draft=$state(favorites.map(f=>({...f})));
 function move(index:number,delta:number){const next=[...draft];[next[index],next[index+delta]]=[next[index+delta],next[index]];draft=next;}
</script>
<Dialog title={t('files.manageFavorites')} {onclose} wide>
 <form onsubmit={event=>{event.preventDefault();onsave(draft.map(f=>({...f,label:f.label?.trim()})));onclose();}}>
 <div class="favorites">{#each draft as item,index (`${item.root}/${item.path}`)}<div class="favorite">
 <label>{t('files.favoriteLabel')}<input class="field" maxlength="100" bind:value={item.label} placeholder={item.path.split('/').at(-1)||item.root}/></label>
 <span class="mono">{item.root}/{item.path}</span><div>
 <button type="button" class="btn" disabled={index===0} aria-label={t('files.favoriteUp',{name:item.label||item.path||item.root})} onclick={()=>move(index,-1)}>↑</button>
 <button type="button" class="btn" disabled={index===draft.length-1} aria-label={t('files.favoriteDown',{name:item.label||item.path||item.root})} onclick={()=>move(index,1)}>↓</button>
 <button type="button" class="btn" onclick={()=>draft=draft.filter((_,i)=>i!==index)}>{t('files.removeFavorite',{name:item.label||item.path||item.root})}</button>
 </div></div>{/each}</div>
 <div class="dialog-actions"><button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button><button class="btn btn-primary">{t('notify.save')}</button></div>
 </form>
</Dialog>
<style>.favorites{max-height:60vh;overflow:auto;}.favorite{display:grid;gap:8px;border-bottom:1px solid var(--border);padding:12px 0;}.favorite span{overflow-wrap:anywhere;}</style>
