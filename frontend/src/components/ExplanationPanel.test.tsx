/**
 * ExplanationPanel: renders the "Почему этот состав" bundle — headline + reason tags,
 * included/excluded/deficit factors and clear counterfactual verification labels (RU).
 */
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import ExplanationPanel, { ExplanationLoading } from './ExplanationPanel';
import type { Explanation } from '../types';
import { reasonCodeLabel } from '../api/errors';

const base: Explanation = {
  variant_id: 'var_a1',
  run_id: 'run_m0001',
  locale: 'ru',
  headline: 'Сбалансированный: состав 5 чел., P50 9.0 нед. / P80 11.3 нед., стоимость 12345 у.е., дефицитов 2.',
  headline_reason_codes: ['VARIANT_BALANCED'],
  tradeoff: {
    text: 'Скорость при P50 9.0 нед. стоит 12345 у.е. и оставляет 2 дефицита.',
    reason_codes: ['TRADEOFF_LOAD_VS_SPEED'],
    facts: { p50_weeks: 9.0, cost_units: 12345, team_size: 5, deficit_count: 2 },
  },
  included: [
    {
      employee_id: 'emp_0001',
      text: 'Бэкенд-разработчик emp_0001: грейд Сеньор (S), уровень 4 ≥ требуемого 4, соответствие 100%, 288 ч.',
      reason_codes: ['INCLUDE_REQUIRED_ROLE', 'INCLUDE_HIGH_COMPETENCE'],
      facts: { level: 4, required_level: 4, competence_fit: 1 },
    },
  ],
  excluded: [
    {
      employee_id: 'emp_0029',
      text: 'emp_0029: недоступен в горизонте планирования (отпуск/отсутствие) — назначения нарушили бы G1.',
      reason_codes: ['EXCLUDE_ABSENT'],
      facts: { absent_weeks: ['2026-W07'] },
    },
  ],
  deficits: [
    {
      competency_id: 'lang_go',
      text: 'Недостаточно «Go»: 12 чел.-дн. (~1.1 нед.) — нужна внешняя кооперация.',
      reason_codes: ['DEFICIT_EXTERNAL_CONTRACTOR'],
      facts: { person_days: 12, weeks: 1.1, external_cooperation_flag: true },
    },
  ],
  counterfactuals: [
    {
      text: 'Если снять ограничение по размеру команды, P50 снизится примерно на 1.1 нед.',
      reason_codes: ['COUNTERFACTUAL_TEAM_SIZE'],
      verified_by: 'estimated',
      facts: { p50_weeks: 9.0, delta_weeks: 1.1 },
    },
    {
      text: 'Пересчёт подтвердил: ограничение изменило состав.',
      reason_codes: ['COUNTERFACTUAL_OVERRIDE'],
      verified_by: 'rerun',
      run_id: 'run_m0002',
      facts: { parent_run_id: 'run_m0001' },
    },
  ],
};

function renderPanel() {
  return render(
    <ConfigProvider locale={ruRU}>
      <ExplanationPanel explanation={base} />
    </ConfigProvider>,
  );
}

describe('ExplanationPanel', () => {
  it('renders the headline with reason-code tags', () => {
    renderPanel();
    expect(screen.getByText(/Сбалансированный: состав 5 чел/)).toBeDefined();
    expect(screen.getAllByText(reasonCodeLabel('VARIANT_BALANCED')).length).toBeGreaterThan(0);
  });

  it('renders included/excluded/deficit factors and their reason tags', async () => {
    renderPanel();
    expect((await screen.findAllByText(/Бэкенд-разработчик Артём Ветров/)).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Ксения Рябова: недоступен/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Недостаточно «Go»/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(reasonCodeLabel('INCLUDE_HIGH_COMPETENCE')).length).toBeGreaterThan(0);
    expect(screen.getAllByText(reasonCodeLabel('EXCLUDE_ABSENT')).length).toBeGreaterThan(0);
    expect(screen.getAllByText(reasonCodeLabel('DEFICIT_EXTERNAL_CONTRACTOR')).length).toBeGreaterThan(0);
  });

  it('labels counterfactual verification distinctly (rerun vs estimated)', () => {
    renderPanel();
    expect(screen.getAllByText(/проверено пересчётом \(run run_m0002\)/).length).toBeGreaterThan(0);
    expect(screen.getAllByText('оценка (не пересчитано)').length).toBeGreaterThan(0);
  });

  it('does not crash on null arrays from a malformed API response', () => {
    const malformed = {
      ...base,
      headline_reason_codes: null,
      excluded: null,
      deficits: null,
      counterfactuals: null,
    } as unknown as Explanation;
    render(
      <ConfigProvider locale={ruRU}>
        <ExplanationPanel explanation={malformed} />
      </ConfigProvider>,
    );
    expect(screen.getByText('Исключены (0)')).toBeDefined();
    expect(screen.getByText('Дефициты (0)')).toBeDefined();
    expect(screen.getByText('Контрфактических сценариев нет.')).toBeDefined();
  });

  it('does not blank the screen when tradeoff is null', () => {
    const malformed = { ...base, tradeoff: null } as unknown as Explanation;
    render(
      <ConfigProvider locale={ruRU}>
        <ExplanationPanel explanation={malformed} />
      </ConfigProvider>,
    );
    expect(screen.getByText(/Сбалансированный: состав 5 чел/)).toBeDefined();
    expect(screen.getByText('Компромисс')).toBeDefined();
    expect(screen.getByText('Включены (1)')).toBeDefined();
  });

  it('renderes the loading placeholder', () => {
    render(<ExplanationLoading />);
    expect(screen.getByText('Почему этот состав')).toBeDefined();
    expect(screen.getByText(/Загружаем обоснование/)).toBeDefined();
  });
});