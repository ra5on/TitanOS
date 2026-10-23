<script lang="ts">import RecoveryPanel from '#lib/operations/RecoveryPanel.svelte';import PageHead from '#lib/components/PageHead.svelte';import ActionSummary from '#lib/operations/ActionSummary.svelte';import {t} from '#lib/i18n/index.svelte.ts';</script>
<PageHead title={t('ops.tasks')} parent={{href:'/tools',label:t('nav.tools')}}/>
<ActionSummary/><RecoveryPanel/>
