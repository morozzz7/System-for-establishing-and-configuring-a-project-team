/**
 * Tests for Layout component rendering.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import AppLayout from '../app/Layout';

describe('Layout', () => {
  it('renders menu with expected items', () => {
    render(
      <ConfigProvider locale={ruRU}>
        <MemoryRouter>
          <AppLayout />
        </MemoryRouter>
      </ConfigProvider>,
    );

    // Check for main menu items
    expect(screen.getByText('Дашборд')).toBeDefined();
    expect(screen.getByText('Сотрудники')).toBeDefined();
    expect(screen.getByText('Проекты')).toBeDefined();
    expect(screen.getByText('Назначения')).toBeDefined();
    expect(screen.getByText('Отсутствия')).toBeDefined();
    expect(screen.getByText('Справочники')).toBeDefined();
  });

  it('renders header with title', () => {
    render(
      <ConfigProvider locale={ruRU}>
        <MemoryRouter>
          <AppLayout />
        </MemoryRouter>
      </ConfigProvider>,
    );

    expect(screen.getByText('Система формирования проектной команды')).toBeDefined();
  });
});
