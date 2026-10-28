<script lang="ts">
	// A QR code drawn as one SVG path, dark on light in every theme
	// (scanners expect that).
	import qrcode from 'qrcode-generator';

	let { text, label, size = 220 }: { text: string; label: string; size?: number } = $props();

	const qr = $derived.by(() => {
		const code = qrcode(0, 'M');
		code.addData(text);
		code.make();
		const count = code.getModuleCount();
		let path = '';
		for (let row = 0; row < count; row++) for (let column = 0; column < count; column++) if (code.isDark(row, column)) path += `M${column},${row}h1v1h-1z`;
		return { count, path };
	});
</script>

<svg class="qr" style:width="{size}px" style:height="{size}px" viewBox="-2 -2 {qr.count + 4} {qr.count + 4}" role="img" aria-label={label}
	><rect x="-2" y="-2" width={qr.count + 4} height={qr.count + 4} class="paper" /><path d={qr.path} class="ink" /></svg
>

<style>
	.qr {
		flex: none;
		max-width: 100%;
		border-radius: var(--radius-ctl);
	}
	.paper {
		fill: var(--qr-light);
	}
	.ink {
		fill: var(--qr-dark);
	}
</style>
