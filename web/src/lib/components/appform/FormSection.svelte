<script lang="ts">
	// A heading between the lines of the app form, aligned with the values.
	let { title, id }: { title: string; id?: string } = $props();
</script>

<h2 class="form-section" {id}><span>{title}</span></h2>

<style>
	.form-section {
		display: grid;
		grid-template-columns: minmax(10rem, 17rem) minmax(0, 1fr);
		gap: 0 var(--space-4);
		margin: 0;
		padding: var(--space-4) var(--space-4) 6px;
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
	}
	span {
		grid-column: 2;
		padding-bottom: 6px;
		border-bottom: 1px solid var(--border);
	}
	@media (max-width: 760px) {
		.form-section {
			grid-template-columns: minmax(0, 1fr);
			padding-inline: var(--space-3);
		}
		span {
			grid-column: 1;
		}
	}
</style>
