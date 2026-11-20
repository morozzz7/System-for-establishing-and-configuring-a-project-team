/**
 * Unit tests for weight validation and normalization logic.
 */
import { describe, it, expect } from 'vitest';
import {
  validateWeights,
  normalizeWeights,
  METRIC_IDS,
  CORE_METRIC_IDS,
  DEFAULT_WEIGHTS,
} from './reference';
import type { MetricDefinition } from './reference';
import type { Weights } from './index';

const mockMetricDefinitions: MetricDefinition[] = [
  {
    id: 'M1',
    name_ru: 'Покрытие компетенций',
    name_en: 'Competency Coverage',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.10, 0.20],
  },
  {
    id: 'M2',
    name_ru: 'Покрытие трудозатрат',
    name_en: 'Effort Coverage',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.10, 0.15],
  },
  {
    id: 'M3',
    name_ru: 'Соответствие уровней',
    name_en: 'Level Fit',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.05, 0.12],
  },
  {
    id: 'M4',
    name_ru: 'Релевантность опыта',
    name_en: 'Experience Relevance',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: false,
    typical_weight_range: [0.05, 0.10],
  },
  {
    id: 'M5',
    name_ru: 'График',
    name_en: 'Schedule',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.08, 0.30],
  },
  {
    id: 'M6',
    name_ru: 'Стоимость',
    name_en: 'Cost',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.04, 0.27],
  },
  {
    id: 'M7',
    name_ru: 'Здоровье нагрузки',
    name_en: 'Load Health',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.05, 0.12],
  },
  {
    id: 'M8',
    name_ru: 'Влияние на портфель',
    name_en: 'Portfolio Impact',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.05, 0.10],
  },
  {
    id: 'M9',
    name_ru: 'Bus-factor',
    name_en: 'Bus Factor',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.03, 0.10],
  },
  {
    id: 'M10',
    name_ru: 'Фокус',
    name_en: 'Focus',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.03, 0.05],
  },
  {
    id: 'M11',
    name_ru: 'Использование бенча',
    name_en: 'Bench Utilization',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.01, 0.03],
  },
  {
    id: 'M12',
    name_ru: 'Устойчивость',
    name_en: 'Resilience',
    formula_ru: '...',
    interpretation_ru: '...',
    is_core: true,
    typical_weight_range: [0.01, 0.10],
  },
];

describe('weight validation', () => {
  it('validates balanced weights correctly', () => {
    const result = validateWeights(DEFAULT_WEIGHTS, mockMetricDefinitions);
    expect(result.valid).toBe(true);
    expect(result.sum).toBeCloseTo(1.0, 6);
    expect(result.errors).toHaveLength(0);
  });

  it('detects sum not equal to 1', () => {
    const weights: Weights = { ...DEFAULT_WEIGHTS, M1: 0.5 }; // sum > 1
    const result = validateWeights(weights, mockMetricDefinitions);
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.includes('Сумма весов'))).toBe(true);
  });

  it('detects missing core metric', () => {
    const weights = { ...DEFAULT_WEIGHTS };
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { M1, ...weightsWithoutM1 } = weights;
    const result = validateWeights(weightsWithoutM1, mockMetricDefinitions);
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.includes('M1'))).toBe(true);
  });

  it('detects weight out of bounds', () => {
    const weights = { ...DEFAULT_WEIGHTS, M1: 1.5 };
    const result = validateWeights(weights, mockMetricDefinitions);
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.includes('M1') && e.includes('диапазоне'))).toBe(true);
  });

  it('warns when weight outside typical range', () => {
    // Adjust weights to keep sum = 1.0
    const weights = { ...DEFAULT_WEIGHTS, M1: 0.01, M5: DEFAULT_WEIGHTS.M5 + 0.14 };
    const result = validateWeights(weights, mockMetricDefinitions);
    expect(result.valid).toBe(true); // warnings don't invalidate
    expect(result.warnings.some((w) => w.includes('M1') && w.includes('типичного диапазона'))).toBe(true);
  });

  it('normalizes weights to sum to 1', () => {
    const weights = { M1: 0.5, M2: 0.3, M3: 0.2 };
    const normalized = normalizeWeights(weights);
    const sum = Object.values(normalized).reduce((a, b) => a + b, 0);
    expect(sum).toBeCloseTo(1.0, 6);
    // Only provided weights are normalized proportionally
    expect(normalized.M1).toBeCloseTo(0.5);
    expect(normalized.M2).toBeCloseTo(0.3);
    expect(normalized.M3).toBeCloseTo(0.2);
  });

  it('returns defaults when all weights are zero', () => {
    const weights = { M1: 0, M2: 0 };
    const normalized = normalizeWeights(weights);
    expect(normalized).toEqual(DEFAULT_WEIGHTS);
  });

  it('preserves optional M4 when provided with valid sum', () => {
    // Adjust to keep sum = 1.0
    const weights = { ...DEFAULT_WEIGHTS, M4: 0.15, M1: DEFAULT_WEIGHTS.M1 - 0.07 };
    const result = validateWeights(weights, mockMetricDefinitions);
    expect(result.valid).toBe(true);
    expect(result.sum).toBeCloseTo(1.0, 6);
  });
});

describe('constants', () => {
  it('METRIC_IDS has all 12 metrics', () => {
    expect(METRIC_IDS).toHaveLength(12);
    expect(METRIC_IDS).toEqual(['M1', 'M2', 'M3', 'M4', 'M5', 'M6', 'M7', 'M8', 'M9', 'M10', 'M11', 'M12']);
  });

  it('CORE_METRIC_IDS excludes M4', () => {
    expect(CORE_METRIC_IDS).toHaveLength(11);
    expect(CORE_METRIC_IDS).not.toContain('M4');
  });

  it('DEFAULT_WEIGHTS sums to 1', () => {
    const sum = Object.values(DEFAULT_WEIGHTS).reduce((a, b) => a + b, 0);
    expect(sum).toBeCloseTo(1.0, 6);
  });
});