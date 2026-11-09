<script lang="ts">
	// PLAN 66: the basic set of web apps (WordCraft, GridCraft, PdfCraft) to
	// choose from – first start, the offer after the update.
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { letterGradient, type WebApp } from './protocol.ts';

	let {
		apps,
		selected = $bindable([]),
		disabled = false,
	}: { apps: WebApp[]; selected?: string[]; disabled?: boolean } = $props();

	// What each program of the basic set is for.
	const KIND: Record<string, MessageKey> = {
		wordcraft: 'webapps.office.kind.wordcraft',
		gridcraft: 'webapps.office.kind.gridcraft',
		pdfcraft: 'webapps.office.kind.pdfcraft',
	};
	function toggle(id: string, on: boolean) {
		selected = on ? [...new Set([...selected, id])] : selected.filter((s) => s !== id);
	}
</script>

<ul class="office">
	{#each apps as a (a.id)}
		<li>
			<label class="pick">
				<input
					type="checkbox"
					checked={selected.includes(a.id) || a.installed}
					disabled={disabled || a.installed}
					onchange={(e) => toggle(a.id, e.currentTarget.checked)}
				/>
				<DesktopIcon
					glyph=""
					gradient={letterGradient(a.id)}
					image={a.icon ?? ''}
					name={a.name}
					size={36}
				/>
				<span class="text">
					<b>{a.name}</b>
					<span class="muted small">
						{#if KIND[a.id]}{t(KIND[a.id])} ·{/if}
						{a.types.map((x) => '.' + x).join(' ')}{#if a.size}
							· {formatBytes(a.size)}{/if}
					</span>
				</span>
				{#if a.installed}<span class="chip ok">{t('webapps.state.ready')}</span>{/if}
			</label>
		</li>
	{/each}
</ul>

<style>
	.office {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.pick {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: calc(10px * var(--radius-scale, 1));
		cursor: pointer;
	}
	.pick:has(input:checked) {
		border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.pick input {
		flex: none;
		width: 18px;
		height: 18px;
	}
	.text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.text .small {
		overflow-wrap: break-word;
	}
	.chip {
		flex: none;
	}
</style>
