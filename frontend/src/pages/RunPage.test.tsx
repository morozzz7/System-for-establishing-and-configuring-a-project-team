/**
 * RunPage: variants are rendered after the run is created and the constraint editor opens.
 * Kept lightweight (no drawer form interaction) — validation logic is covered by utils/constraints.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import RunPage from './RunPage';
import { resetMockState } from '../api/client';

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

describe('RunPage', () => {
  beforeEach(() => {
    resetMockState();
  });

  it('renders the empty state, creates a run and shows variants', async () => {
    renderPage();

    expect(screen.getByText('Варианты команды')).toBeDefined();
    expect(screen.getByText(/Варианты ещё не рассчитаны/)).toBeDefined();

    fireEvent.click(screen.getByRole('button', { name: 'Рассчитать варианты' }));

    await screen.findByText(/Запуск run_/);
    expect(screen.getAllByText('Сбалансированный / приоритет компетенций').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Экономичный / ставка на bench').length).toBeGreaterThan(0);
    // Duration is always a range (P50/P80), never a single "exact" number.
    expect(screen.getAllByText(/P50 .* P80/).length).toBeGreaterThan(0);
    // Key metrics are rendered with their codes.
    expect(screen.getAllByText(/M1 · Покрытие компетенций/).length).toBeGreaterThan(0);
  });

  it('opens the constraint editor with the snapshot hint and a disabled recalculation', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'Рассчитать варианты' }));
    await screen.findByText(/Запуск run_/);

    fireEvent.click(screen.getByRole('button', { name: 'Ограничения' }));

    await screen.findByText('Ограничения и пересчёт');
    expect(screen.getByText('Будет создан новый запуск, текущий не изменится')).toBeDefined();
    expect(screen.getByText('Текущие ограничения запуска')).toBeDefined();
    expect(screen.getByText(/Снимок snp_/)).toBeDefined();

    const recalculateButton = screen.getByRole('button', { name: 'Пересчитать' }) as HTMLButtonElement;
    expect(recalculateButton.disabled).toBe(true);
  });
});
