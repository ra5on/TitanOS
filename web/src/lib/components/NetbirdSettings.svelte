<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 import { formatTime } from '#lib/format.ts';
 import Dialog from './Dialog.svelte';
 let { onpending = (_pending: boolean) => {} } = $props<{ onpending?: (pending: boolean) => void }>();
 type Settings = { managementUrl: string; acceptRoutes: boolean; serveRoutes: boolean; acceptDNS: boolean };
 type Status = { settings: Settings; setupKeySet: boolean; connect: boolean; active: boolean; readOnly: boolean; state: string; version: string; fqdn: string; ips: string[]; management: string; managementConnected: boolean; signalConnected: boolean; peersConnected: number; peersTotal: number; health: string[]; pendingUntil?: string; error?: string };
 let status = $state<Status | null>(null), busy = $state(false), error = $state(''), dirty = $state(false), loggingOut = $state(false);
 let managementUrl = $state(''), acceptRoutes = $state(false), serveRoutes = $state(false), acceptDNS = $state(false), setupKey = $state('');
 const stateKeys = { inactive: 'netbird.states.inactive', Idle: 'netbird.states.Idle', Connecting: 'netbird.states.Connecting', Connected: 'netbird.states.Connected', NeedsLogin: 'netbird.states.NeedsLogin', LoginFailed: 'netbird.states.LoginFailed', SessionExpired: 'netbird.states.SessionExpired' } as const;
 const locked = $derived(busy || !status?.active || status.readOnly || !!status.pendingUntil);
 const online = $derived(status?.state === 'Connected' || status?.state === 'Connecting');
 const needsLogin = $derived(status?.state === 'NeedsLogin' || status?.state === 'LoginFailed' || status?.state === 'SessionExpired');
 function take(value: Status, reset = false) {
  status = value; onpending(!!value.pendingUntil);
  if (reset || !dirty) { const s = value.settings; managementUrl = s.managementUrl; acceptRoutes = s.acceptRoutes; serveRoutes = s.serveRoutes; acceptDNS = s.acceptDNS; dirty = false; }
 }
 function fail(err: unknown) { error = errorText(err instanceof ApiError ? err.code : 'internal') + (err instanceof ApiError ? `\n${err.message}` : ''); }
 // Bumped by every save/control: a poll that started before it answers with the old state and is dropped.
 let generation = 0;
 async function load() { if (busy || document.hidden) return; const asked = generation; try { const value = await api.get<Status>('/plugins/netbird'); if (asked === generation && !busy) take(value); } catch (err) { if (!status && asked === generation) fail(err); } }
 $effect(() => { void load(); const timer = setInterval(load, 5000); return () => { clearInterval(timer); onpending(false); }; });
 async function run(call: () => Promise<Status>, reset = true) { generation++; busy = true; error = ''; try { take(await call(), reset); loggingOut = false; return true; } catch (err) { fail(err); return false; } finally { busy = false; } }
 const action = (name: string) => run(() => api.post<Status>(`/plugins/netbird/control/${name}`, {}));
 // The key leaves the browser once; the field is emptied whatever happens.
 async function saveKey(event: SubmitEvent) { event.preventDefault(); const value = setupKey; setupKey = ''; await run(() => api.put<Status>('/plugins/netbird/setup-key', { setupKey: value }), false); }
 const removeKey = () => run(() => api.put<Status>('/plugins/netbird/setup-key', { setupKey: '' }), false);
 async function save(event: SubmitEvent) { event.preventDefault(); await run(() => api.put<Status>('/plugins/netbird', { managementUrl: managementUrl.trim(), acceptRoutes, serveRoutes, acceptDNS })); }
</script>
<section class="mesh" aria-label="NetBird">
 <h3>{t('netbird.connection')}</h3>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 {#if status}
  <div class="meta"><span class="chip" class:ok={status.state === 'Connected'}>{status.state in stateKeys ? t(stateKeys[status.state as keyof typeof stateKeys]) : status.state}</span><span class="mono">{status.version}</span></div>
  {#if status.fqdn}<strong>{status.fqdn}</strong>{/if}
  {#each status.ips as ip (ip)}<span class="mono address">{ip}</span>{/each}
  {#if status.active && status.management}<p class="muted">{t('netbird.managementState', { url: status.management, state: t(status.managementConnected ? 'netbird.reachable' : 'netbird.unreachable') })}</p>{/if}
  {#if status.state === 'Connected'}<p class="muted">{t('netbird.peerCount', { connected: status.peersConnected, total: status.peersTotal })}</p>{/if}
  {#if status.error}<p class="notice-warn">{status.error}</p>{/if}
  {#each status.health as warning (warning)}<p class="notice-warn">{warning}</p>{/each}
  {#if !status.active}<p class="muted">{t('netbird.activateHint')}</p>{/if}
  {#if needsLogin && !status.setupKeySet}<p class="muted">{t('netbird.keyNeeded')}</p>{/if}
  <div class="actions">
   {#if !online}<button class="btn" disabled={locked || (needsLogin && !status.setupKeySet)} onclick={() => action('connect')}>{t('netbird.connect')}</button>
   {:else}<button class="btn" disabled={locked} onclick={() => action('disconnect')}>{t('netbird.disconnect')}</button>{/if}
   {#if !needsLogin && status.active}<button class="btn" disabled={locked} onclick={() => loggingOut = true}>{t('netbird.logout')}</button>{/if}
  </div>
  {#if status.pendingUntil}
   <div class="notice-warn" role="status">
    <p>{t('netbird.trial', { time: formatTime(new Date(status.pendingUntil)) })}</p>
    <div class="actions"><button class="btn btn-primary" disabled={busy} onclick={() => action('confirm')}>{t('netbird.confirm')}</button><button class="btn" disabled={busy} onclick={() => action('undo')}>{t('netbird.undo')}</button></div>
   </div>
  {/if}
  <form onsubmit={saveKey}>
   <fieldset disabled={busy || status.readOnly || !!status.pendingUntil}>
    <label for="nb-key">{t('netbird.setupKey')}</label>
    <p class="muted">{status.setupKeySet ? t('netbird.keySet') : t('netbird.keyUnset')}</p>
    <input id="nb-key" class="field mono" type="password" autocomplete="off" spellcheck="false" bind:value={setupKey} placeholder={t('netbird.keyPlaceholder')} />
    <p class="muted">{t('netbird.keyHint')}</p>
    <div class="actions"><button class="btn btn-primary" disabled={!setupKey.trim()}>{t('netbird.saveKey')}</button>{#if status.setupKeySet}<button type="button" class="btn" onclick={removeKey}>{t('netbird.removeKey')}</button>{/if}</div>
   </fieldset>
  </form>
  <form onsubmit={save} oninput={() => dirty = true} onchange={() => dirty = true}>
   <fieldset disabled={locked}>
    <label for="nb-management">{t('netbird.management')}</label>
    <input id="nb-management" class="field mono" type="url" bind:value={managementUrl} placeholder="https://api.netbird.io:443" />
    <p class="muted">{t('netbird.managementHint')}</p>
    <label class="check"><input type="checkbox" bind:checked={acceptRoutes} />{t('netbird.acceptRoutes')}</label>
    <label class="check"><input type="checkbox" bind:checked={serveRoutes} />{t('netbird.serveRoutes')}</label>
    <label class="check"><input type="checkbox" bind:checked={acceptDNS} />{t('netbird.acceptDNS')}</label>
    <p class="muted">{t('netbird.policyHint')}</p>
    <button class="btn btn-primary" disabled={!dirty}>{t('netbird.apply')}</button>
   </fieldset>
  </form>
  <details><summary>{t('netbird.firewall')}</summary><p class="muted">{t('netbird.firewallHint')}</p></details>
 {/if}
</section>
{#if loggingOut}<Dialog title={t('netbird.logout')} onclose={() => loggingOut = false}><p>{t('netbird.logoutHint')}</p><div class="dialog-actions"><button type="button" class="btn" onclick={() => loggingOut = false}>{t('common.cancel')}</button><button type="button" class="btn btn-danger" disabled={busy} onclick={() => action('logout')}>{t('netbird.logout')}</button></div></Dialog>{/if}
<style>
 .mesh, fieldset { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
 .mesh { border-top: 1px solid var(--border); padding-top: 20px; }
 fieldset { border: 0; padding: 0; margin: 0; }
 label { font-weight: 500; } .check { display: flex; align-items: center; gap: 10px; }
 .actions, .meta { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
 .mono { font-family: var(--font-mono); } .address { overflow-wrap: anywhere; }
 p { margin: 0; } .notice-error { white-space: pre-wrap; }
 summary { cursor: pointer; } details p { margin-top: 10px; } button { align-self: flex-start; }
</style>
