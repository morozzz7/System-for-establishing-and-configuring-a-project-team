/**
 * Metric presentation helpers (labels are Russian UI text; codes stay machine-readable).
 * Values are always shown as ranges/ratios with their code so the number has a source.
 */
import type { MetricDetails, MetricValues } from '../types';

export interface MetricDescriptor {
  key: keyof MetricValues;
  code: string;
  label: string;
  hint: string;
}

/** Key metrics shown on the variant comparison cards (METRICS.md M1…M12). */
export const KEY_METRICS: MetricDescriptor[] = [
  { key: 'M1', code: 'M1', label: 'Покрытие компетенций', hint: 'Взвешенное покрытие требований уровнями' },
  { key: 'M2', code: 'M2', label: 'Покрытие трудозатрат', hint: 'Доля закрытых человеко-дней' },
  { key: 'M5', code: 'M5', label: 'Соблюдение срока', hint: 'Вероятность уложиться в срок или T_best / T50' },
  { key: 'M6', code: 'M6', label: 'Экономичность', hint: 'Стоимость относительно C_min…C_ref' },
  { key: 'M9', code: 'M9', label: 'Bus factor', hint: 'Дублирование must-компетенций' },
  { key: 'M12', code: 'M12', label: 'Устойчивость', hint: 'Доля сценариев выпадения участника без потери' },
];

export function formatRatio(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return `${Math.round(value * 100)}%`;
}

export function formatWeeks(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return `${value.toFixed(1)} нед.`;
}

export function formatUnits(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return `${Math.round(value)} у.е.`;
}

/** Deficit count is a fact; the label keeps the unit explicit. */
export function formatDeficits(count: number): string {
  return count === 0 ? 'дефицитов нет' : `дефицитов: ${count}`;
}

/** Read a numeric diagnostic out of the open `MetricDetails` record. */
export function metricDetailNumber(details: MetricDetails | undefined, key: string): number | undefined {
  const value = details?.[key];
  return typeof value === 'number' ? value : undefined;
}

/** Read a numeric fact out of the open `facts` record of a recalculation. */
export function factNumber(facts: Record<string, unknown> | undefined, key: string): number | undefined {
  const value = facts?.[key];
  return typeof value === 'number' ? value : undefined;
}

export function factString(facts: Record<string, unknown> | undefined, key: string): string | undefined {
  const value = facts?.[key];
  return typeof value === 'string' ? value : undefined;
}
