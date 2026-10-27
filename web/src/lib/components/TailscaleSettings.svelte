<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 import { formatTime } from '#lib/format.ts';
 import Dialog from './Dialog.svelte';
 let { onpending = (_pending: boolean) => {} } = $props<{ onpending?: (pending: boolean) => void }>();
 type Settings = { routes: string[]; advertiseExit: boolean; exitNode: string; acceptRoutes: boolean; acceptDNS: boolean };
 type Status = { settings: Settings; active: boolean; readOnly: boolean; state: string; version: string; name: string; ips: string[]; authUrl?: string; exitNodes: { id: string; name: string; ip: string; online: boolean }[]; health: string[]; pendingUntil?: string; error?: string };
 let status = $state<Status | null>(null), busy = $state(false), error = $state(''), dirty = $state(false), loggingOut = $state(false);
 let routes = $state(''), mode = $state('none'), exitNode = $state(''), acceptRoutes = $state(false), acceptDNS = $state(false);
 const stateKeys = { inactive: 'tailscale.states.inactive', Running: 'tailscale.states.Running', Stopped: 'tailscale.states.Stopped', NeedsLogin: 'tailscale.states.NeedsLogin', NeedsMachineAuth: 'tailscale.states.NeedsMachineAuth', Starting: 'tailscale.states.Starting', NoState: 'tailscale.states.NoState', InUseOtherUser: 'tailscale.states.InUseOtherUser' } as const;
 const locked = $derived(busy || !status?.active || status.readOnly || !!status.pendingUntil);
 function take(value: Status, reset = false) {
  status = value; onpending(!!value.pendingUntil);
  if (reset || !dirty) { const s = value.settings; routes = s.routes.join('\n'); mode = s.advertiseExit ? 'provide' : s.exitNode ? 'use' : 'none'; exitNode = s.exitNode; acceptRoutes = s.acceptRoutes; acceptDNS = s.acceptDNS; dirty = false; }
 }
 function fail(err: unknown) { error = errorText(err instanceof ApiError ? err.code : 'internal') + (err instanceof ApiError ? `\n${err.message}` : ''); }
 // Bumped by every save/control: a poll that started before it answers with the old state and is dropped.
 let generation = 0;
 async function load() { if (busy || document.hidden) return; const asked = generation; try { const value = await api.get<Status>('/plugins/tailscale'); if (asked === generation && !busy) take(value); } catch (err) { if (!status && asked === generation) fail(err); } }
 $effect(() => { void load(); const timer = setInterval(load, 5000); return () => { clearInterval(timer); onpending(false); }; });
 async function action(name: string) { generation++; busy = true; error = ''; try { take(await api.post<Status>(`/plugins/tailscale/control/${name}`, {}), true); loggingOut = false; } catch (err) { fail(err); } finally { busy = false; } }
 async function save(event: SubmitEvent) {
  event.preventDefault(); generation++; busy = true; error = '';
  try { take(await api.put<Status>('/plugins/tailscale', { routes: routes.split(/[\s,]+/).filter(Boolean), advertiseExit: mode === 'provide', exitNode: mode === 'use' ? exitNode : '', acceptRoutes, acceptDNS }), true); }
  catch (err) { fail(err); } finally { busy = false; }
 }
</script>
<section class="tailnet" aria-label="Tailscale">
 <h3>{t('tailscale.connection')}</h3>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 {#if status}
  <div class="meta"><span class="chip" class:ok={status.state === 'Running'}>{status.state in stateKeys ? t(stateKeys[status.state as keyof typeof stateKeys]) : status.state}</span><span class="mono">{status.version}</span></div>
  {#if status.name}<strong>{status.name}</strong>{/if}
  {#each status.ips as ip (ip)}<span class="mono address">{ip}</span>{/each}
  {#if status.error}<p class="notice-warn">{status.error}</p>{/if}
  {#each status.health as warning (warning)}<p class="notice-warn">{warning}</p>{/each}
  {#if !status.active}<p class="muted">{t('tailscale.activateHint')}</p>{/if}
  {#if status.authUrl}<a class="btn btn-primary" href={status.authUrl} target="_blank" rel="noopener noreferrer">{t('tailscale.login')}</a>{/if}
  <div class="actions">
   {#if status.state !== 'Running'}<button class="btn" disabled={locked} onclick={() => action('connect')}>{t('tailscale.connect')}</button>
   {:else}<button class="btn" disabled={locked} onclick={() => action('disconnect')}>{t('tailscale.disconnect')}</button>{/if}
   {#if status.state !== 'NeedsLogin' && status.active}<button class="btn" disabled={locked} onclick={() => loggingOut = true}>{t('tailscale.logout')}</button>{/if}
  </div>
  {#if status.pendingUntil}
   <div class="notice-warn" role="status">
    <p>{t('tailscale.trial', { time: formatTime(new Date(status.pendingUntil)) })}</p>
    <div class="actions"><button class="btn btn-primary" disabled={busy} onclick={() => action('confirm')}>{t('tailscale.confirm')}</button><button class="btn" disabled={busy} onclick={() => action('undo')}>{t('tailscale.undo')}</button></div>
   </div>
  {/if}
  <form onsubmit={save} oninput={() => dirty = true} onchange={() => dirty = true}>
   <fieldset disabled={locked}>
    <label for="ts-routes">{t('tailscale.routes')}</label>
    <textarea id="ts-routes" class="field mono" rows="3" bind:value={routes} placeholder="192.168.10.0/24"></textarea>
    <p class="muted">{t('tailscale.routesHint')}</p>
    <label for="ts-exit-mode">{t('tailscale.exitMode')}</label>
    <select id="ts-exit-mode" class="field" bind:value={mode}><option value="none">{t('tailscale.exitNone')}</option><option value="provide">{t('tailscale.exitProvide')}</option><option value="use">{t('tailscale.exitUse')}</option></select>
    {#if mode === 'use'}
     <label for="ts-exit">{t('tailscale.exitNode')}</label>
     <select id="ts-exit" class="field" bind:value={exitNode} required>
      <option value="">{t('tailscale.chooseExit')}</option>
      {#if exitNode && !status.exitNodes.some(peer => peer.ip === exitNode)}<option value={exitNode}>{exitNode} · {t('tailscale.unavailable')}</option>{/if}
      {#each status.exitNodes as peer (peer.id)}<option value={peer.ip}>{peer.name} · {peer.ip}{peer.online ? '' : ` · ${t('tailscale.offline')}`}</option>{/each}
     </select>
     <p class="muted">{t('tailscale.lanAccess')}</p>
    {/if}
    <label class="check"><input type="checkbox" bind:checked={acceptRoutes} />{t('tailscale.acceptRoutes')}</label>
    <label class="check"><input type="checkbox" bind:checked={acceptDNS} />{t('tailscale.acceptDNS')}</label>
    <p class="muted">{t('tailscale.approvalHint')}</p>
    <button class="btn btn-primary" disabled={!dirty || (mode === 'use' && !exitNode)}>{t('tailscale.apply')}</button>
   </fieldset>
  </form>
  <details><summary>{t('tailscale.firewall')}</summary><p class="muted">{t('tailscale.firewallHint')}</p></details>
 {/if}
</section>
{#if loggingOut}<Dialog title={t('tailscale.logout')} onclose={() => loggingOut = false}><p>{t('tailscale.logoutHint')}</p><div class="dialog-actions"><button type="button" class="btn" onclick={() => loggingOut = false}>{t('common.cancel')}</button><button type="button" class="btn btn-danger" disabled={busy} onclick={() => action('logout')}>{t('tailscale.logout')}</button></div></Dialog>{/if}
<style>
 .tailnet, fieldset { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
 .tailnet { border-top: 1px solid var(--border); padding-top: 20px; }
 fieldset { border: 0; padding: 0; margin: 0; }
 label { font-weight: 500; } .check { display: flex; align-items: center; gap: 10px; }
 .actions, .meta { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
 .mono { font-family: var(--font-mono); } .address { overflow-wrap: anywhere; }
 p { margin: 0; } textarea { resize: vertical; } .notice-error { white-space: pre-wrap; }
 summary { cursor: pointer; } details p { margin-top: 10px; } button { align-self: flex-start; }
</style>
