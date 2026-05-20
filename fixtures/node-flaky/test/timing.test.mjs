import { test, expect } from 'vitest';

// Intentional flake: fails when the event loop is blocked for >40ms during
// the 50ms window. Used as a fixture for ReproForge's flake detector.
test('eventually resolves within 50ms', async () => {
    const t0 = Date.now();
    await new Promise((resolve) => setTimeout(resolve, 50));
    const dt = Date.now() - t0;
    // Tight bound — flakes under load.
    expect(dt).toBeLessThan(60);
});
