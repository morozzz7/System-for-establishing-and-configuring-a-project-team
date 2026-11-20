/**
 * Reference data types for weight presets and metric definitions.
 * Matches contracts/openapi.yaml schemas:
 * - WeightPresetListResponse
 * - MetricDefinitionsResponse
 * - MetricDefinition
 * - MetricInfo
 */

import type { Weights as BaseWeights, PresetName } from './index';

/** Re-export Weights for consumers of this module. */
export type Weights = BaseWeights;

/** Short metric info for the weight editor (from /reference/weight-presets). */
export interface MetricInfo {
  name_ru: string;
  name_en: string;
  description_ru: string;
}

/** Full metric definition with formula and interpretation (from /reference/metrics). */
export interface MetricDefinition {
  id: string; // M1..M13
  name_ru: string;
  name_en: string;
  formula_ru: string;
  interpretation_ru: string;
  is_core: boolean;
  typical_weight_range: [number, number];
}

/** Response for GET /api/v1/reference/weight-presets */
export interface WeightPresetListResponse {
  presets: WeightPreset[];
  metrics: Record<string, MetricInfo>;
}

/** Extended weight preset with description for UI cards. */
export interface WeightPreset {
  preset: PresetName;
  label_ru: string;
  description_ru: string;
  weights: Weights;
  sum_check: number;
}

/** Response for GET /api/v1/reference/metrics */
export interface MetricDefinitionsResponse {
  metrics: MetricDefinition[];
}

/** All metric IDs in order M1..M12 (M4 optional, M13 experimental). */
export const METRIC_IDS: Array<keyof Weights> = [
  'M1', 'M2', 'M3', 'M4', 'M5', 'M6', 'M7', 'M8', 'M9', 'M10', 'M11', 'M12',
];

/** Core metrics (always included in Score). */
export const CORE_METRIC_IDS: Array<keyof Weights> = [
  'M1', 'M2', 'M3', 'M5', 'M6', 'M7', 'M8', 'M9', 'M10', 'M11', 'M12',
];

/** Optional metric (excluded when no project history). */
export const OPTIONAL_METRIC_IDS: Array<keyof Weights> = ['M4'];

/** Default balanced weights for fallback/reset. */
export const DEFAULT_WEIGHTS: Weights = {
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

/** Weight validation result. */
export interface WeightValidationResult {
  valid: boolean;
  sum: number;
  errors: string[];
  warnings: string[];
}

/** Validate a complete weights object. */
export function validateWeights(weights: Partial<Weights>, metricDefinitions: MetricDefinition[] = []): WeightValidationResult {
  const errors: string[] = [];
  const warnings: string[] = [];

  // Check all required metrics present
  for (const id of CORE_METRIC_IDS) {
    if (weights[id] === undefined || weights[id] === null) {
      errors.push(`Отсутствует вес для ${id}`);
    }
  }

  // Calculate sum
  let sum = 0;
  for (const id of METRIC_IDS) {
    const value = weights[id] ?? 0;
    if (value < 0 || value > 1) {
      errors.push(`${id}: вес должен быть в диапазоне [0, 1]`);
    }
    sum += value;
  }

  // Check sum = 1.0 ± 1e-6
  if (Math.abs(sum - 1.0) > 1e-6) {
    errors.push(`Сумма весов = ${sum.toFixed(6)}, должна быть 1.000000`);
  }

  // Check typical ranges (warnings only)
  if (metricDefinitions.length > 0) {
    const defMap = new Map(metricDefinitions.map((d) => [d.id, d.typical_weight_range]));
    for (const [id, range] of defMap) {
      const value = weights[id as keyof Weights];
      if (value !== undefined && value !== null && (value < range[0] || value > range[1])) {
        warnings.push(`${id} (${value.toFixed(3)}): вне типичного диапазона [${range[0]}, ${range[1]}]`);
      }
    }
  }

  return {
    valid: errors.length === 0,
    sum,
    errors,
    warnings,
  };
}

/** Normalize weights to sum to 1.0 (proportional). Only provided metrics are included. */
export function normalizeWeights(weights: Partial<Weights>): Weights {
  const total = Object.values(weights).reduce((sum, v) => sum + (v ?? 0), 0);
  if (total === 0) return DEFAULT_WEIGHTS;

  const result: Weights = { ...DEFAULT_WEIGHTS };
  // Reset all to 0 first
  for (const id of METRIC_IDS) {
    result[id] = 0;
  }
  // Normalize provided weights
  for (const id of METRIC_IDS) {
    const value = weights[id];
    if (value !== undefined && value !== null) {
      result[id] = value / total;
    }
  }
  return result;
}