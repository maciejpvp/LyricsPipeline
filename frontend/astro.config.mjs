import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  integrations: [react()],
  output: 'static',
  devToolbar: { enabled: false },
  publicDir: fileURLToPath(new URL('../mocks/', import.meta.url)),
});
