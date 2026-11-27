import { describe, expect, it } from 'vitest';
import { formatDate, formatWeekRange } from './date';

describe('Russian calendar dates', () => {
  it('keeps date-only values and renders the ISO-week boundary', () => {
    expect(formatDate('2026-11-27')).toBe('27 ноября 2026');
    expect(formatWeekRange('2025-W01')).toBe('30 декабря 2024 — 5 января 2025');
  });
});
