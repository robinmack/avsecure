import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'build', // keep 'build' so deploy script needs no changes
  },
  test: {
    environment: 'jsdom',
    globals: true,   // vi, describe, test, expect available without imports
  },
});
