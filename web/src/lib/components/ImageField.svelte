<script lang="ts">
	import { imageReference, validImage, withImageTag } from '#lib/compose/image.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	let { image = $bindable(), onchange = () => {} }: { image: string; onchange?: () => void } =
		$props();
	const ref = $derived(imageReference(image));
</script>

<div class="image-fields">
	<label
		><span>{t('editor.image')}</span><input
			class="field mono"
			type="text"
			bind:value={image}
			oninput={onchange}
			spellcheck="false"
			aria-invalid={!validImage(image)}
		/></label
	>
	<label
		><span>{t('editor.imageTag')}</span><input
			class="field mono"
			type="text"
			value={ref.tag}
			disabled={!ref.simple}
			oninput={(event) => {
				image = withImageTag(image, event.currentTarget.value);
				onchange();
			}}
			spellcheck="false"
			aria-invalid={ref.simple && !validImage(image)}
		/></label
	>
	{#if !ref.simple}<small>{t('editor.imageReferenceHint')}</small>{/if}
	{#if !validImage(image)}<small class="warn" role="alert">{t('editor.imageInvalid')}</small>{/if}
</div>

<style>
	.image-fields {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 12px;
		min-width: 0;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1 1 120px;
		min-width: 0;
	}
	label:first-child {
		flex: 3 1 220px;
	}
	small {
		flex-basis: 100%;
		color: var(--muted);
	}
	small.warn {
		color: var(--warn);
	}
</style>
