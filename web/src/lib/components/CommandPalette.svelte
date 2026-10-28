<script lang="ts">
 import { onMount, tick } from 'svelte';
 import { goto } from '$app/navigation';
 import { api, type DockerStatus, type FileRoot, type VM } from '#lib/api.ts';
 import { session } from '#lib/session.svelte.ts';
 import { t } from '#lib/i18n/index.svelte.ts';
 import { findPaletteEntries, palettePages, type PaletteEntry } from '#lib/command-palette.ts';
 import { assistant } from '#lib/assistant.svelte.ts';
 import { recentPages, rememberPage } from '#lib/palette.svelte.ts';
 import { containerLabel, type LXCList } from '#lib/lxc.ts';
 import Dialog from './Dialog.svelte';
 let {onclose}:{onclose:()=>void}=$props();
 let query=$state(''), dynamic=$state<PaletteEntry[]>([]), loading=$state(false), partial=$state(false), index=$state(0);
 let disposed=false;
 const role=$derived(session.data?.user?.role??'user');
 const all=$derived([...palettePages(role,t,{assistant:!!assistant.status?.available}),...dynamic]);
 // Without a query, the pages opened last in this page session come first (RAM only).
 const recent=$derived(query.trim()?[]:recentPages.list.map(href=>all.find(e=>e.href===href)).filter((e):e is PaletteEntry=>!!e).map(e=>({...e,section:t('palette.recent')})));
 const entries=$derived([...recent,...findPaletteEntries(all,query).filter(e=>!recent.some(r=>r.href===e.href))].slice(0,60));
 const isMac=typeof navigator!=='undefined'&&/Mac|iPhone|iPad/.test(navigator.platform);
 onMount(()=>{void load();return()=>{disposed=true;};});
 async function load(){
  if(loading)return;loading=true;partial=false;
  const admin=role==='owner'||role==='admin';
  const result=await Promise.allSettled([
   api.get<FileRoot[]>('/files/roots'),
   admin?api.get<DockerStatus>('/docker'):Promise.resolve(null),
   admin?api.get<{vms:VM[]}>('/vms'):Promise.resolve(null),
   admin?api.get<LXCList>('/lxc'):Promise.resolve(null)
  ]);
  if(disposed)return;
  const next:PaletteEntry[]=[];
  if(result[0].status==='fulfilled')for(const root of result[0].value.filter(r=>r.kind==='share'))next.push({href:'/files?'+new URLSearchParams({root:root.id}),label:root.name,section:t('nav.shares')});
  if(result[1].status==='fulfilled'&&result[1].value){const apps=new Set<string>();for(const c of result[1].value.containers)if(c.managed&&c.app&&!apps.has(c.app)){apps.add(c.app);next.push({href:'/docker/apps/'+encodeURIComponent(c.app),label:c.app,section:t('nav.docker')});}}
  if(result[2].status==='fulfilled'&&result[2].value)for(const vm of result[2].value.vms)next.push({href:'/vms?'+new URLSearchParams({vm:vm.name}),label:vm.displayName||vm.name,section:t('nav.vms')});
  if(result[3].status==='fulfilled'&&result[3].value)for(const c of result[3].value.containers)next.push({href:'/lxc?'+new URLSearchParams({ct:c.name}),label:containerLabel(c),section:t('nav.lxc')});
  dynamic=next;partial=result.some(r=>r.status==='rejected');loading=false;index=0;
 }
 function visit(entry:PaletteEntry){rememberPage(entry.href);onclose();void goto(entry.href);}
 async function keys(e:KeyboardEvent){
  if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();index=Math.max(0,Math.min(entries.length-1,index+(e.key==='ArrowDown'?1:-1)));await tick();document.getElementById('palette-result-'+index)?.scrollIntoView({block:'nearest'});}
  else if(e.key==='Enter'){e.preventDefault();if(entries[index])visit(entries[index]);}
 }
</script>
<Dialog title={t('palette.title')} {onclose}>
 <label class="search"><span>{t('palette.search')}</span><!-- svelte-ignore a11y_autofocus --><input class="field" type="search" role="combobox" aria-expanded={entries.length>0} aria-autocomplete="list" aria-controls="palette-results" aria-activedescendant={entries[index]?'palette-result-'+index:undefined} bind:value={query} oninput={()=>index=0} onkeydown={keys} autofocus/></label>
 <p class="keys muted">{t('palette.keys',{open:isMac?'⌘K':'Ctrl+K'})}</p>
 {#if loading}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
 {#if partial}<p class="notice-warn" role="alert">{t('palette.partial')} <button class="btn" disabled={loading} onclick={load}>{t('common.retry')}</button></p>{/if}
 <div id="palette-results" class="results" role="listbox" aria-label={t('palette.results')}>
 {#each entries as entry,i(entry.section+entry.href)}<div id={'palette-result-'+i} role="option" tabindex="-1" aria-selected={i===index} class="result" class:current={i===index} onclick={()=>visit(entry)} onkeydown={e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();visit(entry);}}} onmousemove={()=>index=i}><span>{entry.label}</span><small>{entry.section}</small></div>{/each}
 </div>
 {#if entries.length===0}<p class="muted" role="status">{t('palette.empty')}</p>{/if}
 <div class="dialog-actions"><button class="btn" onclick={onclose}>{t('common.cancel')}</button></div>
</Dialog>
<style>
 .search{display:grid;gap:6px;font-size:.8125rem}.results{display:flex;flex-direction:column;gap:3px;max-height:50dvh;overflow:auto}.results .result{display:flex;justify-content:space-between;align-items:center;gap:16px;min-height:40px;text-align:left;font:inherit;font-size:.875rem;background:none;border:0;border-radius:var(--radius-ctl);padding:8px 10px;color:var(--text);cursor:pointer}.results .result.current,.results .result:hover{background:var(--chip)}.results .result span{overflow-wrap:anywhere}.results small{color:var(--muted);font-size:.75rem;text-align:right}p{font-size:.8125rem}.keys{margin:0;font-size:.75rem}
</style>
