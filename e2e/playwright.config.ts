import { defineConfig } from '@playwright/test'

// Runs against the live stack: ./scripts/demo.sh first (gateway :8880, web UI :5188).
export default defineConfig({
  testDir: '.',
  timeout: 60_000,
  workers: 1, // the learn run owns the shared fault/traffic switches; never two at once
  reporter: [['list']],
  use: { baseURL: process.env.TAILHUNT_UI || 'http://localhost:5188', headless: true, viewport: { width: 1440, height: 900 } },
})
