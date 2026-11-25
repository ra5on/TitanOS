<svg class="trail" aria-hidden="true" focusable="false">
	{#each [0, 1, 2] as layer (layer)}
		<rect pathLength="100" rx="13" class:tip={layer === 2} class:middle={layer === 1}></rect>
	{/each}
</svg>

<style>
	.trail {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		pointer-events: none;
		overflow: visible;
	}
	rect {
		x: 1px;
		y: 1px;
		width: calc(100% - 2px);
		height: calc(100% - 2px);
		fill: none;
		stroke: var(--ag-edge, var(--accent));
		stroke-width: 1.5px;
		stroke-linecap: round;
		stroke-dasharray: 7 93;
		opacity: 0.28;
		animation: orbit var(--ag-orbit, 4.8s) linear infinite;
	}
	.middle {
		stroke-dasharray: 3 97;
		opacity: 0.55;
		animation-delay: calc(-0.04 * var(--ag-orbit, 4.8s));
	}
	.tip {
		stroke-dasharray: 0.6 99.4;
		opacity: 1;
		animation-delay: calc(-0.064 * var(--ag-orbit, 4.8s));
	}
	@keyframes orbit {
		to {
			stroke-dashoffset: -100;
		}
	}
	:global(.agent.calm) .trail,
	:global(.agent[data-trail='off']) .trail {
		display: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.trail {
			display: none;
		}
	}
</style>
