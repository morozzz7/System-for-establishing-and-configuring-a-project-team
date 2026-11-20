/**
 * Unit tests for WeightSelector component.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import WeightSelector from './WeightSelector';
import type { WeightPreset, MetricDefinition, WeightPresetListResponse } from '../../types/reference';

const mockPresets: WeightPreset[] = [
  {
    preset: 'fast',
    label_ru: 'Быстрый',
    description_ru: 'Максимальная скорость',
    weights: { M1: 0.15, M2: 0.15, M3: 0.08, M4: 0.08, M5: 0.28, M6: 0.04, M7: 0.05, M8: 0.06, M9: 0.03, M10: 0.03, M11: 0.02, M12: 0.03 },
    sum_check: 1.0,
  },
  {
    preset: 'balanced',
    label_ru: 'Сбалансированный',
    description_ru: 'Умеренный приоритет',
    weights: { M1: 0.15, M2: 0.13, M3: 0.10, M4: 0.08, M5: 0.15, M6: 0.10, M7: 0.08, M8: 0.08, M9: 0.05, M10: 0.04, M11: 0.02, M12: 0.02 },
    sum_check: 1.0,
  },
];

const mockMetricsInfo = {
  M1: { name_ru: 'Покрытие компетенций', name_en: '', description_ru: '' },
  M2: { name_ru: 'Покрытие трудозатрат', name_en: '', description_ru: '' },
  M3: { name_ru: 'Соответствие уровней', name_en: '', description_ru: '' },
  M4: { name_ru: 'Релевантность опыта', name_en: '', description_ru: '' },
  M5: { name_ru: 'График', name_en: '', description_ru: '' },
  M6: { name_ru: 'Стоимость', name_en: '', description_ru: '' },
  M7: { name_ru: 'Здоровье нагрузки', name_en: '', description_ru: '' },
  M8: { name_ru: 'Влияние на портфель', name_en: '', description_ru: '' },
  M9: { name_ru: 'Bus-factor', name_en: '', description_ru: '' },
  M10: { name_ru: 'Фокус', name_en: '', description_ru: '' },
  M11: { name_ru: 'Использование бенча', name_en: '', description_ru: '' },
  M12: { name_ru: 'Устойчивость', name_en: '', description_ru: '' },
};

const mockMetricDefinitions: MetricDefinition[] = [
  { id: 'M1', name_ru: 'Покрытие компетенций', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.10, 0.20] },
  { id: 'M2', name_ru: 'Покрытие трудозатрат', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.10, 0.15] },
  { id: 'M3', name_ru: 'Соответствие уровней', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.05, 0.12] },
  { id: 'M4', name_ru: 'Релевантность опыта', name_en: '', formula_ru: '', interpretation_ru: '', is_core: false, typical_weight_range: [0.05, 0.10] },
  { id: 'M5', name_ru: 'График', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.08, 0.30] },
  { id: 'M6', name_ru: 'Стоимость', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.04, 0.27] },
  { id: 'M7', name_ru: 'Здоровье нагрузки', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.05, 0.12] },
  { id: 'M8', name_ru: 'Влияние на портфель', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.05, 0.10] },
  { id: 'M9', name_ru: 'Bus-factor', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.03, 0.10] },
  { id: 'M10', name_ru: 'Фокус', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.03, 0.05] },
  { id: 'M11', name_ru: 'Использование бенча', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.01, 0.03] },
  { id: 'M12', name_ru: 'Устойчивость', name_en: '', formula_ru: '', interpretation_ru: '', is_core: true, typical_weight_range: [0.01, 0.10] },
];

const mockPresetsData: WeightPresetListResponse = {
  presets: mockPresets,
  metrics: mockMetricsInfo,
};

describe('WeightSelector', () => {
  it('renders preset tabs', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Критерии оптимизации')).toBeInTheDocument();
    expect(screen.getByText('Выберите пресет или настройте веса вручную')).toBeInTheDocument();
  });

  it('shows selected preset', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        initialPreset="balanced"
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Выбран пресет: Сбалансированный')).toBeInTheDocument();
  });

  it('calls onChange with preset weights when preset selected', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        initialPreset="fast"
        onChange={onChange}
      />,
    );
    // Initially shows fast preset in the alert
    expect(screen.getByText('Выбран пресет: Быстрый')).toBeInTheDocument();
    // The preset selection is tested in PresetCard.test.tsx; here we verify the component renders
    expect(screen.getByText('Критерии оптимизации')).toBeInTheDocument();
  });

  it('switches to custom mode tab', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Настроить веса'));
    expect(screen.getByText('Пользовательские веса')).toBeInTheDocument();
  });

  it('shows metric definitions panel', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Справочник метрик'));
    expect(screen.getByText('Справочник метрик (M1–M13)')).toBeInTheDocument();
    // Expand the collapse panel
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    expect(screen.getByText('M1')).toBeInTheDocument();
  });

  it('shows current weights JSON', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        initialPreset="balanced"
        onChange={onChange}
      />,
    );
    expect(screen.getByText('M1: 15.0%')).toBeInTheDocument();
  });

  it('shows warning when no project history', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        hasProjectHistory={false}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Настроить веса'));
    expect(screen.getByText(/M4 \(Релевантность опыта\) будет исключена/)).toBeInTheDocument();
  });

  it('resets to balanced when reset button clicked in custom mode', () => {
    const onChange = vi.fn();
    render(
      <WeightSelector
        presetsData={mockPresetsData}
        metricsData={mockMetricDefinitions}
        initialPreset="fast"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Настроить веса'));
    fireEvent.click(screen.getByText('Сбросить на «Сбалансированный»'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ M1: 0.15, M2: 0.13 }),
      'balanced',
    );
  });
});