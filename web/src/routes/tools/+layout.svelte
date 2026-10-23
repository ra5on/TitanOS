<script lang="ts">import type {Snippet} from 'svelte';import {session} from '#lib/session.svelte.ts';import {t} from '#lib/i18n/index.svelte.ts';let {children}:{children:Snippet}=$props();const admin=$derived(['owner','admin'].includes(session.data?.user?.role??''));</script>
{#if admin}{@render children()}{:else}<p class="notice-error" role="alert">{t('error.forbidden')}</p>{/if}
