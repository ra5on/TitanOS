<script lang="ts">
	// PLAN 67: the dock's settings are the tab "Dock" of Darstellung; old
	// links and bookmarks of this page lead there.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { t } from '#lib/i18n/index.svelte.ts';

	onMount(() => {
		void goto('/settings/appearance?tab=dock', { replaceState: true });
	});
</script>

<p class="muted" role="status">{t('common.loading')}</p>
