<script lang="ts">
	// Stub of the skeleton (PLAN 54); the group fills this component: its
	// rows in the list of functions and the settings of each function.
	import type { Features, FeaturesMine, FeaturesPublic } from '#lib/agent-features.ts';
	type Props = {
		admin: boolean;
		nas: Features | null;
		pub: FeaturesPublic | null;
		mine: FeaturesMine | null;
		savenas: (next: Features) => Promise<void>;
		savemine: (next: FeaturesMine) => Promise<void>;
		calm?: boolean;
	};
	const props: Props = $props();
	void props;
</script>
