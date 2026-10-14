<script lang="ts">
 import { onMount } from 'svelte';
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 type Device = { id: string; title: string; kind: 'igpu' | 'gpu' | 'npu' | 'nvidia' | 'coral'; vendor?: string; available: boolean; reason: string; warning?: string };
 let { selected = $bindable<string[]>([]), onchange = () => {} }: { selected: string[]; onchange?: () => void } = $props();
 let devices = $state<Device[]>([]);
 let error = $state('');
 onMount(() => { api.get<Device[]>('/hardware/container-devices').then(value => { devices = value; selected = [...new Set(selected.map(id => value.find(d => d.available && (id === 'gpu-nvidia' && d.kind === 'nvidia' || id === 'gpu-intel' && d.vendor === '8086' && ['igpu','gpu'].includes(d.kind) || id === 'npu-intel' && d.vendor === '8086' && d.kind === 'npu'))?.id ?? id))]; }, err => error = errorText(err instanceof ApiError ? err.code : 'internal')); });
 function toggle(id: string) { selected = selected.includes(id) ? selected.filter(value => value !== id) : [...selected, id]; onchange(); }
</script>
<fieldset>
 <legend>{t('hardware.containerTitle')}</legend>
 <p class="muted">{t('hardware.containerHint')}</p>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 {#each devices as device (device.id)}
  <label class="check"><input type="checkbox" checked={selected.includes(device.id)} disabled={!device.available && !selected.includes(device.id)} onchange={() => toggle(device.id)} />
   <span>{device.title} · {t(device.kind === 'nvidia' ? 'hardware.kind.gpu' : device.kind === 'coral' ? 'hardware.coralKind' : `hardware.kind.${device.kind}`)}{#if device.warning === 'usb_bus'} — {t('hardware.coralUSBWarning')}{/if}{#if !device.available} — {t(device.reason === 'vm' ? 'hardware.inVm' : 'hardware.noDriver')}{/if}</span>
  </label>
 {/each}
 {#each selected.filter(id => !devices.some(device => device.id === id)) as id (id)}
  <label class="check"><input type="checkbox" checked onchange={() => toggle(id)} /><span class="mono">{id}</span></label>
 {/each}
 {#if !devices.length && !selected.length && !error}<p class="muted">{t('hardware.nonePci')}</p>{/if}
</fieldset>
<style>
 fieldset { border: 0; padding: 0; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
 legend { font-weight: 600; margin-bottom: 10px; }
 p, label { font-size: 0.875rem; }
</style>
