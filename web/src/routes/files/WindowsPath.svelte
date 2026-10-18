<script lang="ts">
 import {api, type FileRoot, type Share} from '#lib/api.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 import {t} from '#lib/i18n/index.svelte.ts';
 import {system} from '#lib/system.svelte.ts';
 import {windowsFolder, windowsMapping, windowsCopy} from '#lib/windows-path.ts';
 let {root, dir, onclose}: {root: FileRoot; dir: string; onclose: () => void} = $props();
 let shares = $state<Share[] | null>(null);
 let failed = $state(false);
 let source = $state('');
 let drive = $state('Z');
 const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
 const target = $derived(shares ? windowsFolder(root, dir, shares, host) : null);
 const command = $derived(target ? windowsCopy(source, target) : '');
 $effect(() => { let disposed = false; api.get<Share[]>('/shares').then(value => {if (!disposed) shares = value;}).catch(() => {if (!disposed) failed = true;}); return () => {disposed = true;}; });
</script>
<Dialog title={t('files.windowsPath')} wide {onclose}>
 <div class="content">
 {#if failed}<p class="notice-error">{t('files.windowsLoadFailed')}</p>
 {:else if !shares}<p>…</p>
 {:else if !target}<p class="notice-warn">{t('files.windowsUnavailable')}</p>
 {:else}
 <label>{t('files.windowsFolder')}<input class="field mono" readonly value={target} onfocus={event => event.currentTarget.select()} /></label>
 <p>{t('files.windowsMapHint')}</p>
 <label>{t('files.windowsDrive')}<select class="field drive" bind:value={drive}>{#each 'DEFGHIJKLMNOPQRSTUVWXYZ' as letter}<option value={letter}>{letter}:</option>{/each}</select></label>
 <textarea class="field mono" readonly rows="3" aria-label={t('files.windowsMapCommand')} value={windowsMapping(target, drive)} onfocus={event => event.currentTarget.select()}></textarea>
 <p>{t('files.windowsCopyHint')}</p>
 <label>{t('files.windowsSource')}<input class="field mono" bind:value={source} placeholder={'C:\\Fotos'} spellcheck="false" /></label>
 {#if command}<textarea class="field mono" readonly rows="4" aria-label={t('files.windowsCopyCommand')} value={command} onfocus={event => event.currentTarget.select()}></textarea>{/if}
 <p class="muted">{t('files.windowsLimits')}</p>
 {/if}
 </div>
 <div class="dialog-actions"><button class="btn btn-primary" type="button" onclick={onclose}>{t('common.done')}</button></div>
</Dialog>
<style>
 .content { display: flex; flex-direction: column; gap: 12px; font-size: .875rem; }
 label { display: flex; flex-direction: column; gap: 6px; }
 textarea { height: auto; padding: 10px; resize: vertical; overflow-wrap: anywhere; }
 .drive { width: 6rem; }
</style>
