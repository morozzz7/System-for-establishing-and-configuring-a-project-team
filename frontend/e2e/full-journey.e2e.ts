import { readFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';

const fixture = (name: string) => path.resolve(process.cwd(), 'e2e', 'fixtures', name);

test('full journey: import pool -> brief -> run -> compare -> recalc -> decide', async ({ page }) => {
  const runtimeErrors: string[] = [];
  page.on('pageerror', (error) => runtimeErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().includes('favicon')) {
      runtimeErrors.push(message.text());
    }
  });

  // 1. Import the employee pool through the UI (real PUT /api/v1/employees).
  await page.goto('/employees');
  await expect(page.getByRole('heading', { name: 'Сотрудники' })).toBeVisible();
  await page.getByRole('button', { name: 'Загрузить данные' }).click();
  await page.locator('.ant-modal input[type=file]').setInputFiles(fixture('employees.json'));
  await expect(page.getByText('Загружено записей: 8')).toBeVisible();
  await page.getByRole('button', { name: 'Проверить (Dry Run)' }).click();
  await expect(page.getByText(/Dry Run успешен/)).toBeVisible();
  await page.getByRole('button', { name: /Импортировать \(8\)/ }).click();
  await expect(page.locator('.ant-modal-wrap').first()).toBeHidden();
  await expect(page.getByText('emp_e2e01').first()).toBeVisible();

  // 2. Upload a brief; the ML service extracts requirements via the recorded LLM response.
  await page.getByRole('button', { name: 'Новое ТЗ' }).click();
  await expect(page).toHaveURL(/\/briefs$/);
  await page
    .getByPlaceholder('Например: Перевод личного кабинета на микросервисы')
    .fill('E2E: MVP веб-сервиса управления задачами');
  await page
    .getByPlaceholder('Вставьте исходный текст технического задания заказчика...')
    .fill(readFileSync(fixture('brief.md'), 'utf8'));
  await page.getByRole('button', { name: /Извлечь требования/ }).click();
  await expect(page).toHaveURL(/\/briefs\/brf_[a-z0-9]+\/review/, { timeout: 90_000 });

  // 3. Review and confirm the extracted requirements.
  await expect(page.getByRole('heading', { name: 'Проверка требований' })).toBeVisible();
  await expect(page.locator('.ant-list-item').first()).toBeVisible();
  await expect(page.getByText(/Бэкенд-разработчик/).first()).toBeVisible();
  await page.getByRole('button', { name: 'Подтвердить и настроить расчёт' }).click();
  await expect(page).toHaveURL(/\/runs\/config\/brf_/);

  // 4. Configure weights and start the optimization run.
  await expect(page.getByRole('heading', { name: 'Настройка расчёта вариантов' })).toBeVisible();
  // A 14-week horizon gives each fully available candidate enough person-days to clear
  // the Go validator's likely-effort gate (G3) for this brief.
  const startWeekInput = page.getByTestId('horizon-start-week').locator('input');
  await startWeekInput.fill('2026-W10');
  await startWeekInput.press('Enter');
  await expect(page.getByTestId('horizon-start-week').locator('.ant-select-selection-item')).toHaveText('2026-W10');
  const endWeekInput = page.getByTestId('horizon-end-week').locator('input');
  await endWeekInput.fill('2026-W20');
  await endWeekInput.press('Enter');
  await expect(page.getByTestId('horizon-end-week').locator('.ant-select-selection-item')).toHaveText('2026-W20');
  await page.getByRole('button', { name: 'Рассчитать варианты' }).click();
  await expect(page).toHaveURL(/\/runs\/new\/brf_[^?]+\?run=run_/, { timeout: 90_000 });

  // 5. Compare variants and inspect the member roster of the second one.
  await expect(page.getByRole('heading', { name: 'Варианты команды' })).toBeVisible();
  await expect(page.getByText(/^Состав: \d+$/)).toHaveCount(2, { timeout: 90_000 });
  await expect(page.getByText(/P50 /).first()).toBeVisible();
  await page.getByText(/^Состав: \d+$/).nth(1).click();
  await expect(page.locator('[data-testid="variant-member-id"]').first()).toBeVisible({
    timeout: 30_000,
  });
  const memberId = (
    await page.locator('[data-testid="variant-member-id"]').first().textContent()
  )?.trim();
  expect(memberId).toMatch(/^emp_[a-z0-9]{4,12}$/);

  // 6. Apply a hard constraint and recalculate into a new child run.
  await page.getByRole('button', { name: 'Ограничения' }).click();
  // The editor defaults to force_exclude, so only the employee id is needed.
  await page.getByRole('combobox', { name: 'Сотрудник' }).fill(memberId!);
  await page.getByRole('button', { name: 'Добавить ограничение' }).click();
  await expect(page.getByText('Добавленные ограничения (1)')).toBeVisible();
  await page.getByRole('button', { name: 'Пересчитать' }).click();
  await expect(page.locator('[data-testid="recalc-result"]')).toBeVisible({ timeout: 90_000 });
  await expect(
    page.locator('[data-testid="recalc-result"]').getByText(/Пересчёт применён/).first(),
  ).toBeVisible();
  await expect(page.getByText(/родительский запуск run_/)).toBeVisible();

  // 7. Human-in-the-loop: record the final decision.
  await page.getByRole('button', { name: 'Утвердить состав' }).click();
  await page.getByRole('button', { name: 'Подтвердить решение' }).click();
  await expect(page.locator('[data-testid="decision-success"]')).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/Состав команды утверждён \(аудит /)).toBeVisible();

  expect(runtimeErrors).toEqual([]);
});
