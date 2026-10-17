<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { sound, armSound, playSound } from '#lib/sound.svelte.ts';
 import { tones, sanitizeSound } from '#lib/sounds.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 let draft = $state(sanitizeSound(sound.settings));
 let error = $state('');
 let saved = $state(false);
 let busy = $state(false);
 async function save() {
  busy = true; error = ''; saved = false;
  try { const value = sanitizeSound(draft); await api.put('/preferences/sound', value); sound.settings = value; saved = true; }
  catch (err) { error = errorText(err instanceof ApiError ? err.code : 'internal'); }
  finally { busy = false; }
 }
 async function preview() {
  error = '';
  const previous = sound.settings;
  sound.settings = sanitizeSound(draft);
  const ready = await armSound();
  sound.settings = previous;
  if (!ready || !playSound(sanitizeSound(draft))) error = t('sound.blocked');
 }
</script>
<section class="tile sounds">
 <h2>{t('sound.title')}</h2><p class="muted">{t('sound.hint')}</p>
 <div class="row">
 <label><span>{t('sound.tone')}</span><select class="field" bind:value={draft.tone} onchange={() => saved = false}><option value="none">{t('sound.none')}</option>{#each tones as tone}<option value={tone}>{t(`sound.${tone}`)}</option>{/each}</select></label>
 <label><span>{t('sound.volume')} · {draft.volume}%</span><input type="range" min="0" max="100" step="5" bind:value={draft.volume} oninput={() => saved = false} /></label>
 <label><span>{t('sound.when')}</span><select class="field" bind:value={draft.severity} onchange={() => saved = false}><option value="important">{t('sound.important')}</option><option value="all">{t('sound.all')}</option></select></label>
 </div>
 <div class="row"><button class="btn" type="button" onclick={preview} disabled={draft.tone === 'none' || draft.volume === 0}>{t('sound.preview')}</button><button class="btn btn-primary" type="button" onclick={save} disabled={busy}>{t('common.save')}</button>{#if saved}<span role="status">{t('sound.saved')}</span>{/if}</div>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>
<style>
 .sounds { padding: var(--tile-pad); margin-block: var(--gap); }
 .row { display:flex; flex-wrap:wrap; gap:var(--gap); margin-block:var(--gap); align-items:center; }
 label { display:flex; flex-direction:column; gap:.4rem; min-width:0; flex:1 1 12rem; }
 h2 { font-size:1rem; }
</style>
