import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cssVariables, dataAttributes, defaultAppearance, effectOn, sanitizeAppearance } from '../src/lib/theme/themes.ts';
import { rotateHue } from '../src/lib/theme/color.ts';

test('effects: older appearances get the defaults, broken values are ignored', () => {
	const old = sanitizeAppearance({ theme: 'hell' });
	assert.deepEqual(old.fx, defaultAppearance().fx);
	assert.equal(old.fx.pulse, false);
	assert.equal(old.fx.gradient, false);
	const broken = sanitizeAppearance({ fx: { pulse: 'yes', gradient: true, micro: 0, extra: true } });
	assert.equal(broken.fx.pulse, false);
	assert.equal(broken.fx.gradient, true);
	assert.equal(broken.fx.micro, true);
	assert.equal('extra' in broken.fx, false);
});

test('effects: the main switch turns every effect off', () => {
	const a = sanitizeAppearance({ fx: { enabled: false, transitions: true, gradient: true } });
	assert.equal(effectOn(a, 'transitions'), false);
	const attrs = dataAttributes(a, false);
	for (const id of ['transitions', 'skeleton', 'micro', 'pulse', 'gradient', 'illustrations', 'shortcuts']) {
		assert.equal(attrs[`data-fx-${id}`], 'off');
	}
	assert.doesNotMatch(cssVariables(a, false)['--accent-fill'], /gradient/);
});

test('effects: gradient accent uses two hues', () => {
	const a = sanitizeAppearance({ fx: { gradient: true } });
	assert.match(cssVariables(a, false)['--accent-fill'], /^linear-gradient\(135deg, #[0-9A-Fa-f]{6}, #[0-9a-f]{6}\)$/);
	assert.equal(rotateHue('#808080', 40), '#808080');
	assert.equal(rotateHue('#ff0000', 120), '#00ff00');
});
