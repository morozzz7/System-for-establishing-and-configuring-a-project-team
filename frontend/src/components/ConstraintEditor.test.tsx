import { mockEmployees } from '../mocks/reference';
/**
 * ConstraintEditor wiring: draft → contract override → POST recalculate → result callback.
 * The mock backend is used, so this also proves the editor is contract-compatible end to end.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import ConstraintEditor from './ConstraintEditor';
import { createRun, getRunInputs, getVariant, resetMockState } from '../api/client';
import type { RecalculateResponse, RunInputs, TeamRun } from '../types';

const UI_TIMEOUT = 8000;

async function prepare(): Promise<{ run: TeamRun; inputs: RunInputs; memberId: string }> {
  const run = await createRun('brf_test');
  const inputs = await getRunInputs(run.run_id);
  const variant = await getVariant(run.run_id, run.variants[0].variant_id);
  if (!inputs || !variant) throw new Error('mock snapshot missing');
  return { run, inputs, memberId: variant.members[0].employee_id };
}

function employeeName(id: string): string {
  return mockEmployees.find((employee) => employee.employee_id === id)?.display_name ?? 'Имя не указано';
}

async function selectEmployee(value: string): Promise<void> {
  const input = screen.getByRole('combobox', { name: 'Сотрудник' });
  fireEvent.mouseDown(input);
  fireEvent.change(input, { target: { value: employeeName(value) } });
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(employeeName(value)) }));
}

async function clickButton(name: string): Promise<void> {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name }));
  });
}

describe('ConstraintEditor', () => {
  beforeEach(() => {
    resetMockState();
  });

  it('adds an override and recalculates through the mock API', async () => {
    const { run, inputs, memberId } = await prepare();
    const onRecalculated = vi.fn<(response: RecalculateResponse, newRun: TeamRun) => void>();
    const onError = vi.fn();

    render(
      <ConfigProvider locale={ruRU}>
        <ConstraintEditor
          open
          run={run}
          inputs={inputs}
          inputsError={null}
          baseVariantId={run.variants[0].variant_id}
          onClose={() => undefined}
          onRecalculated={onRecalculated}
          onError={onError}
        />
      </ConfigProvider>,
    );

    // Default draft type is force_exclude; exclude a member of the selected variant.
    await selectEmployee(memberId);
    await clickButton('Добавить ограничение');

    expect(await screen.findByText(new RegExp(`Исключить ${employeeName(memberId)}`), {}, { timeout: UI_TIMEOUT })).toBeDefined();

    await clickButton('Пересчитать');

    await vi.waitFor(() => expect(onRecalculated).toHaveBeenCalledTimes(1), { timeout: UI_TIMEOUT });
    const [response] = onRecalculated.mock.calls[0];
    expect(response.reason_codes).toContain('RECALC_APPLIED');
    expect(response.facts?.changed_members).toBeGreaterThan(0);
    expect(onError).not.toHaveBeenCalled();
  }, 15000);

  it('blocks recalculation for contradictory overrides', async () => {
    const { run, inputs, memberId } = await prepare();

    render(
      <ConfigProvider locale={ruRU}>
        <ConstraintEditor
          open
          run={run}
          inputs={inputs}
          inputsError={null}
          baseVariantId={run.variants[0].variant_id}
          onClose={() => undefined}
          onRecalculated={() => undefined}
          onError={() => undefined}
        />
      </ConfigProvider>,
    );

    await selectEmployee(memberId);
    await clickButton('Добавить ограничение');
    await screen.findByText(new RegExp(`Исключить ${employeeName(memberId)}`), {}, { timeout: UI_TIMEOUT });

    // A second override of the same employee is a duplicate: the action must stay disabled.
    await selectEmployee(memberId);
    await clickButton('Добавить ограничение');

    const duplicateNotice = await screen.findByText(/Дубликат: Исключить сотрудника/, {}, { timeout: UI_TIMEOUT });
    expect(duplicateNotice).toBeDefined();
  }, 15000);

  it('blocks including an employee who is unavailable for the whole horizon', async () => {
    const { run, inputs } = await prepare();
    const unavailable = inputs.candidates.find((candidate) => candidate.capacity_weeks === 0);
    expect(unavailable).toBeDefined();

    render(
      <ConfigProvider locale={ruRU}>
        <ConstraintEditor
          open
          run={run}
          inputs={inputs}
          inputsError={null}
          onClose={() => undefined}
          onRecalculated={() => undefined}
          onError={() => undefined}
        />
      </ConfigProvider>,
    );

    await selectEmployee(unavailable?.employee_id ?? '');
    await clickButton('Добавить ограничение');

    const warning = await screen.findByText(/недоступен весь горизонт/, {}, { timeout: UI_TIMEOUT });
    expect(warning).toBeDefined();
    // The override was not added, so the recalculation stays disabled.
    const recalculate = screen.getByRole('button', { name: 'Пересчитать' }) as HTMLButtonElement;
    expect(recalculate.disabled).toBe(true);
  }, 15000);
});
