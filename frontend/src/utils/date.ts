import { parseWeek, weekEnd, weekStart } from './week';

const dateFormat = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC',
});

export function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : dateFormat.format(date).replace(/\sг\.$/, '');
}

export function formatWeekDate(value: string, end = false): string {
  const week = parseWeek(value);
  return week ? formatDate((end ? weekEnd : weekStart)(week.year, week.week).toISOString()) : '—';
}

export function formatWeekRange(value: string): string {
  return parseWeek(value) ? `${formatWeekDate(value)} — ${formatWeekDate(value, true)}` : '—';
}
