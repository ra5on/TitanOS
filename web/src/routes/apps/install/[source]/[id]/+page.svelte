<script lang="ts">
	import { page } from '$app/state';
	import InstallPage from '../../../InstallPage.svelte';
</script>

{#key `${page.params.source}/${page.params.id}`}
	<InstallPage source={page.params.source} id={page.params.id} />
{/key}
