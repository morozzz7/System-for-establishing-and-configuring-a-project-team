/**
 * Unit tests for CustomWeightsEditor component.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import CustomWeightsEditor from './CustomWeightsEditor';
import type { MetricDefinition } from '../../types/reference';
import type { Weights } from '../../types/index';

const mockWeights: Weights = {
  M1: 0.15,
  M2: 0.13,
  M3: 0.10,
  M4: 0.08,
  M5: 0.15,
  M6: 0.10,
  M7: 0.08,
  M8: 0.08,
  M9: 0.05,
  M10: 0.04,
  M11: 0.02,
  M12: 0.02,
};

const mockMetricDefinitions: MetricDefinition[] = [
  { id: 'M1', name_ru: 'Покрытие компетенций', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.10, 0.20] },
  { id: 'M2', name_ru: 'Покрытие трудозатрат', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.10, 0.15] },
  { id: 'M3', name_ru: 'Соответствие уровней', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.05, 0.12] },
  { id: 'M4', name_ru: 'Релевантность опыта', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: false, typical_weight_range: [0.05, 0.10] },
  { id: 'M5', name_ru: 'График', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.08, 0.30] },
  { id: 'M6', name_ru: 'Стоимость', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.04, 0.27] },
  { id: 'M7', name_ru: 'Здоровье нагрузки', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.05, 0.12] },
  { id: 'M8', name_ru: 'Влияние на портфель', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.05, 0.10] },
  { id: 'M9', name_ru: 'Bus-factor', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.03, 0.10] },
  { id: 'M10', name_ru: 'Фокус', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.03, 0.05] },
  { id: 'M11', name_ru: 'Использование бенча', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.01, 0.03] },
  { id: 'M12', name_ru: 'Устойчивость', name_en: '', formula_ru: '', interpretation_ru: 'desc', is_core: true, typical_weight_range: [0.01, 0.10] },
];

describe('CustomWeightsEditor', () => {
  it('renders all metric sliders', () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('M1')).toBeInTheDocument();
    expect(screen.getByText('M12')).toBeInTheDocument();
  });

  it('shows validation success when weights sum to 1', () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Веса валидны')).toBeInTheDocument();
    // The sum text may be split across elements, use a flexible matcher
    expect(screen.getByText((content) => content.includes('Сумма:') && content.includes('1.000000'))).toBeInTheDocument();
  });

  it('shows validation error when weights sum != 1', () => {
    const badWeights = { ...mockWeights, M1: 0.5 }; // sum > 1
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={badWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Веса невалидны')).toBeInTheDocument();
    expect(screen.getByText(/Сумма весов = .*, должна быть 1\.000000/)).toBeInTheDocument();
  });

  it('calls onChange when InputNumber changes', async () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    // Find the first InputNumber (for M1) and change it
    const inputNumbers = screen.getAllByRole('spinbutton');
    expect(inputNumbers.length).toBeGreaterThan(0);
    fireEvent.change(inputNumbers[0], { target: { value: 50 } }); // 50%
    await waitFor(() => {
      expect(onChange).toHaveBeenCalled();
    });
    const calledWeights = onChange.mock.calls[0][0] as Record<string, number>;
    // After normalization, M1 should be > 0.3 (proportional to 50% of total)
    expect(calledWeights.M1).toBeGreaterThan(0.3);
    // Sum should be 1.0
    const sum = Object.values(calledWeights).reduce((a: number, b: number) => a + b, 0);
    expect(sum).toBeCloseTo(1.0, 6);
  });

  it('shows optional badge for M4 when no project history', () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={false}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('опционально (нет истории)')).toBeInTheDocument();
  });

  it('disables M4 when no project history', () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={false}
        onChange={onChange}
      />,
    );
    const sliders = screen.getAllByRole('slider');
    // M4 is the 4th metric (index 3)
    const m4Slider = sliders[3];
    expect(m4Slider).toHaveAttribute('aria-disabled', 'true');
  });

  it('shows typical range tooltips', () => {
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('[10% – 20%]')).toBeInTheDocument(); // M1
    expect(screen.getByText('[1% – 3%]')).toBeInTheDocument(); // M11
  });

  it('calls onReset when reset button clicked', () => {
    const onChange = vi.fn();
    const onReset = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={mockWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
        onReset={onReset}
      />,
    );
    fireEvent.click(screen.getByText('Сбросить на «Сбалансированный»'));
    expect(onReset).toHaveBeenCalled();
    expect(onChange).toHaveBeenCalled();
  });

  it('shows warning when weight outside typical range', () => {
    const badWeights = { ...mockWeights, M1: 0.01 }; // below typical [0.10, 0.20]
    const onChange = vi.fn();
    render(
      <CustomWeightsEditor
        initialWeights={badWeights}
        metricDefinitions={mockMetricDefinitions}
        hasProjectHistory={true}
        onChange={onChange}
      />,
    );
    expect(screen.getByText(/M1 .* вне типичного диапазона/)).toBeInTheDocument();
  });
});