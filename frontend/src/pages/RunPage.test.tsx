/**
 * RunPage: variants are rendered after the run is created and the constraint editor opens.
 * Kept lightweight (no drawer form interaction) — validation logic is covered by utils/constraints.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import RunPage from './RunPage';
import { listProjects, resetMockState } from '../api/client';

const UI_TIMEOUT = 10000;

function renderPage() {
  return render(
    <ConfigProvider locale={ruRU}>
      <MemoryRouter initialEntries={['/runs/new/brf_test']}>
        <Routes>
          <Route path="/runs/new/:briefId" element={<RunPage />} />
        </Routes>
      </MemoryRouter>
    </ConfigProvider>,
  );
}

async function clickButton(name: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name }));
  });
}

describe('RunPage', () => {
  beforeEach(() => {
    resetMockState();
  });

  it('renders the empty state, creates a run and shows variants', async () => {
    renderPage();

    expect(screen.getByText('Варианты команды')).toBeDefined();
    expect(screen.getByText(/Варианты ещё не рассчитаны/)).toBeDefined();

    await clickButton('Рассчитать варианты');

    await screen.findByText(/Запуск run_/, {}, { timeout: UI_TIMEOUT });
    expect(screen.getAllByText('Сбалансированный / приоритет компетенций').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Экономичный / ставка на bench').length).toBeGreaterThan(0);
    // Duration is always a range (P50/P80), never a single "exact" number.
    expect(screen.getAllByText(/P50 .* P80/).length).toBeGreaterThan(0);
    // Key metrics are rendered with their codes.
    expect(screen.getAllByText(/M1 · Покрытие компетенций/).length).toBeGreaterThan(0);
  }, 15000);

  it('adds the approved team to the projects portfolio', async () => {
    renderPage();
    await clickButton('Рассчитать варианты');
    await screen.findByText(/Запуск run_/, {}, { timeout: UI_TIMEOUT });

    await clickButton('Утвердить состав');
    await screen.findByText(/Вы утверждаете вариант/, {}, { timeout: UI_TIMEOUT });
    await clickButton('Подтвердить решение');

    await screen.findByTestId('decision-success', {}, { timeout: UI_TIMEOUT });
    // The decision alert renders before the derived project is stored; poll the mock store directly.
    await vi.waitFor(async () => {
      const projects = await listProjects();
      expect(projects.map((p) => p.project_id)).toContain('prj_test');
    }, { timeout: UI_TIMEOUT, interval: 100 });
  }, 15000);

  it('opens the constraint editor with the snapshot hint and a disabled recalculation', async () => {
    renderPage();
    await clickButton('Рассчитать варианты');
    await screen.findByText(/Запуск run_/, {}, { timeout: UI_TIMEOUT });

    await clickButton('Ограничения');

    await screen.findByText('Ограничения и пересчёт', {}, { timeout: UI_TIMEOUT });
    expect(screen.getByText('Будет создан новый запуск, текущий не изменится')).toBeDefined();
    expect(screen.getByText('Текущие ограничения запуска')).toBeDefined();
    expect(screen.getByText(/Снимок snp_/)).toBeDefined();

    const recalculateButton = screen.getByRole('button', { name: 'Пересчитать' }) as HTMLButtonElement;
    expect(recalculateButton.disabled).toBe(true);
  }, 15000);
});
