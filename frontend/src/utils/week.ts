/**
 * ISO 8601 week utilities (ADR-002).
 * Weeks are Monday-based: 2026-W07, 2026-W52, etc.
 */

const WEEK_REGEX = /^(\d{4})-W(\d{2})$/;

/**
 * Parse a week key string into year and week number.
 * Returns null if the format is invalid.
 */
export function parseWeek(weekKey: string): { year: number; week: number } | null {
  const match = weekKey.match(WEEK_REGEX);
  if (!match) return null;
  const year = parseInt(match[1], 10);
  const week = parseInt(match[2], 10);
  if (week < 1 || week > 53) return null;
  return { year, week };
}

/**
 * Format a year and week number into a week key string.
 */
export function formatWeek(year: number, week: number): string {
  if (week < 1 || week > 53) {
    throw new Error(`Invalid week number: ${week}`);
  }
  return `${year}-W${String(week).padStart(2, '0')}`;
}

/**
 * Get the Monday of a given ISO week as a Date object.
 */
export function weekStart(year: number, week: number): Date {
  // Jan 4 is always in week 1 of the ISO year
  const jan4 = new Date(Date.UTC(year, 0, 4));
  const dayOfWeek = jan4.getUTCDay() || 7; // 0=Sun -> 7
  const mondayOfWeek1 = new Date(jan4);
  mondayOfWeek1.setUTCDate(jan4.getUTCDate() - dayOfWeek + 1);

  const targetMonday = new Date(mondayOfWeek1);
  targetMonday.setUTCDate(mondayOfWeek1.getUTCDate() + (week - 1) * 7);
  return targetMonday;
}

/**
 * Get the Sunday of a given ISO week as a Date object.
 */
export function weekEnd(year: number, week: number): Date {
  const monday = weekStart(year, week);
  const sunday = new Date(monday);
  sunday.setUTCDate(monday.getUTCDate() + 6);
  return sunday;
}

/**
 * Get the date range (Monday to Sunday) of a week as a formatted string.
 */
export function weekRange(weekKey: string): string {
  const parsed = parseWeek(weekKey);
  if (!parsed) return '';
  const { year, week } = parsed;
  const start = weekStart(year, week);
  const end = weekEnd(year, week);
  const fmt = (d: Date) =>
    `${d.getUTCDate().toString().padStart(2, '0')}.${(d.getUTCMonth() + 1).toString().padStart(2, '0')}`;
  return `${fmt(start)}–${fmt(end)}`;
}

/**
 * Check if a string is a valid week key.
 */
export function isValidWeek(weekKey: string): boolean {
  return parseWeek(weekKey) !== null;
}

/**
 * Get the current ISO week key based on the current date.
 */
export function currentWeek(): string {
  const now = new Date();
  const utcDate = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()));
  const dayNum = utcDate.getUTCDay() || 7;
  const nearestThursday = new Date(utcDate);
  nearestThursday.setUTCDate(utcDate.getUTCDate() + (4 - dayNum));
  const yearStart = new Date(Date.UTC(nearestThursday.getUTCFullYear(), 0, 1));
  const weekNum = Math.ceil(((nearestThursday.getTime() - yearStart.getTime()) / 86400000 + 1) / 7);
  return formatWeek(nearestThursday.getUTCFullYear(), weekNum);
}
