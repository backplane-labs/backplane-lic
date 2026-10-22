import { defineConfig } from 'vitest/config';

export default defineConfig({
  // The HTTP handler injects <base href="configured portal path/"> before these assets.
  base: './',
  build: { outDir: 'dist', emptyOutDir: true },
  test: { environment: 'jsdom', clearMocks: true },
});
