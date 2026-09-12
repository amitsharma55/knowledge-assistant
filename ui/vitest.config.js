import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config.js';

// Reuses the app's Vite config (React plugin, JSX transform) so tests compile
// exactly as the app does; only the DOM environment is test-specific.
export default mergeConfig(viteConfig, defineConfig({ test: { environment: 'jsdom' } }));
