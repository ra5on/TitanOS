<script lang="ts">
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import DiskTests from './DiskTests.svelte';
</script>

<PageHead title={t('disktest.title')} parent={{ href: '/tools', label: t('nav.tools') }} />
<DiskTests />
