import { defineConfig, devices } from '@playwright/test';

const gatewayPort = process.env.GATEWAY_PORT ?? '9080';
const baseURL = process.env.E2E_BASE_URL ?? `http://127.0.0.1:${gatewayPort}`;

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  timeout: 180_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
