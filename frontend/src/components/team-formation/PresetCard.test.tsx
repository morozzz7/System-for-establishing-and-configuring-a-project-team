/**
 * Unit tests for PresetCard component.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PresetCard from './PresetCard';
import type { WeightPreset } from '../../types/reference';

const mockPreset: WeightPreset = {
  preset: 'balanced',
  label_ru: 'Сбалансированный',
  description_ru: 'Умеренный приоритет скорости, качества покрытия и стоимости',
  weights: {
    M1: 0.15,
    M2: 0.13,
    M3: 0.1,
    M5: 0.15,
    M6: 0.1,
    M7: 0.08,
    M8: 0.08,
    M9: 0.05,
    M10: 0.04,
    M11: 0.02,
    M12: 0.02,
  },
  sum_check: 1.0,
};

const mockMetrics = {
  M1: { name_ru: 'Покрытие компетенций' },
  M2: { name_ru: 'Покрытие трудозатрат' },
};

describe('PresetCard', () => {
  it('renders preset label and description', () => {
    render(<PresetCard preset={mockPreset} selected={false} onSelect={vi.fn()} metrics={mockMetrics} />);
    expect(screen.getByText('Сбалансированный')).toBeInTheDocument();
    expect(screen.getByText('Умеренный приоритет скорости, качества покрытия и стоимости')).toBeInTheDocument();
  });

  it('keeps the selected badge inside the card header', () => {
    render(<PresetCard preset={mockPreset} selected={true} onSelect={vi.fn()} />);
    const badge = screen.getByText('Выбран');
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveStyle({ margin: '0px', flexShrink: '0', whiteSpace: 'nowrap', fontSize: '9px' });
    const titleRow = badge.closest('.ant-card-head-title')?.firstElementChild as HTMLElement | null;
    expect(titleRow).toHaveStyle({ gap: '6px' });
    expect(titleRow?.getAttribute('style')).toContain('min-width: 0');
  });

  it('calls onSelect when clicked', () => {
    const onSelect = vi.fn();
    render(<PresetCard preset={mockPreset} selected={false} onSelect={onSelect} />);
    fireEvent.click(screen.getByText('Сбалансированный'));
    expect(onSelect).toHaveBeenCalledWith('balanced');
  });

  it('shows weight tags', () => {
    render(<PresetCard preset={mockPreset} selected={false} onSelect={vi.fn()} metrics={mockMetrics} />);
    expect(screen.getByText('M1 15%')).toBeInTheDocument();
    expect(screen.getByText('M2 13%')).toBeInTheDocument();
  });

  it('shows sum check', () => {
    render(<PresetCard preset={mockPreset} selected={false} onSelect={vi.fn()} />);
    expect(screen.getByText('Σ = 1.000000')).toBeInTheDocument();
  });

  it('renders bar charts for weights', () => {
    render(<PresetCard preset={mockPreset} selected={false} onSelect={vi.fn()} />);
    // Check that bar containers exist (divs with background style)
    const bars = screen.getAllByText(/^\d+%$/);
    expect(bars.length).toBeGreaterThan(0);
  });
});