/**
 * Tests for week utilities (ISO 8601).
 */
import { describe, it, expect } from 'vitest';
import { parseWeek, formatWeek, weekRange, isValidWeek, currentWeek } from '../utils/week';

describe('week utils', () => {
  describe('parseWeek', () => {
    it('parses valid week keys', () => {
      expect(parseWeek('2026-W07')).toEqual({ year: 2026, week: 7 });
      expect(parseWeek('2025-W52')).toEqual({ year: 2025, week: 52 });
      expect(parseWeek('2026-W01')).toEqual({ year: 2026, week: 1 });
    });

    it('returns null for invalid formats', () => {
      expect(parseWeek('')).toBeNull();
      expect(parseWeek('2026-07')).toBeNull();
      expect(parseWeek('W07')).toBeNull();
      expect(parseWeek('2026-W00')).toBeNull(); // Week 0 is invalid
      expect(parseWeek('2026-W54')).toBeNull(); // Week 54 is invalid
      expect(parseWeek('abc')).toBeNull();
    });
  });

  describe('formatWeek', () => {
    it('formats year and week into week key', () => {
      expect(formatWeek(2026, 7)).toBe('2026-W07');
      expect(formatWeek(2025, 52)).toBe('2025-W52');
      expect(formatWeek(2026, 1)).toBe('2026-W01');
    });

    it('throws for invalid week numbers', () => {
      expect(() => formatWeek(2026, 0)).toThrow();
      expect(() => formatWeek(2026, 54)).toThrow();
    });
  });

  describe('weekRange', () => {
    it('returns date range string for valid week', () => {
      const range = weekRange('2026-W07');
      // W07 of 2026 starts on Monday Feb 9
      expect(range).toContain('09.02');
      expect(range).toContain('15.02');
    });

    it('returns empty string for invalid week', () => {
      expect(weekRange('invalid')).toBe('');
    });
  });

  describe('isValidWeek', () => {
    it('returns true for valid weeks', () => {
      expect(isValidWeek('2026-W07')).toBe(true);
      expect(isValidWeek('2025-W52')).toBe(true);
    });

    it('returns false for invalid weeks', () => {
      expect(isValidWeek('')).toBe(false);
      expect(isValidWeek('2026-W00')).toBe(false);
      expect(isValidWeek('2026-W54')).toBe(false);
    });
  });

  describe('currentWeek', () => {
    it('returns a valid week key', () => {
      const current = currentWeek();
      expect(isValidWeek(current)).toBe(true);
      // Should be 2026-W45 or similar (current date is Nov 7, 2026)
      expect(current).toMatch(/^\d{4}-W\d{2}$/);
    });
  });
});
