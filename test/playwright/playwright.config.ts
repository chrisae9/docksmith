import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './specs',
  fullyParallel: false, // Run tests sequentially (they share container state)
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1, // Single worker for container tests
  reporter: 'html',
  timeout: 60000, // 60s timeout for container operations
  expect: {
    timeout: 10000, // 10s for assertions
  },

  use: {
    baseURL: process.env.DOCKSMITH_URL || 'https://docksmith.ts.chis.dev',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
