/**
 * ConstraintEditor wiring: draft → contract override → POST recalculate → result callback.
 * The mock backend is used, so this also proves the editor is contract-compatible end to end.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import ConstraintEditor from './ConstraintEditor';
import { createRun, getRunInputs, getVariant, resetMockState } from '../api/client';
import type { RecalculateResponse, RunInputs, TeamRun } from '../types';

async function prepare(): Promise<{ run: TeamRun; inputs: RunInputs; memberId: string }> {
  const run = await createRun('brf_test');
  const inputs = await getRunInputs(run.run_id);
  const variant = await getVariant(run.run_id, run.variants[0].variant_id);
  if (!inputs || !variant) throw new Error('mock snapshot missing');
  return { run, inputs, memberId: variant.members[0].employee_id };
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
    const employeeInput = screen.getByRole('combobox', { name: 'Сотрудник' });
    fireEvent.change(employeeInput, { target: { value: memberId } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить ограничение' }));

    expect(await screen.findByText(new RegExp(`Исключить ${memberId}`))).toBeDefined();

    fireEvent.click(screen.getByRole('button', { name: 'Пересчитать' }));

    await vi.waitFor(() => expect(onRecalculated).toHaveBeenCalledTimes(1), { timeout: 3000 });
    const [response] = onRecalculated.mock.calls[0];
    expect(response.reason_codes).toContain('RECALC_APPLIED');
    expect(response.facts?.changed_members).toBeGreaterThan(0);
    expect(onError).not.toHaveBeenCalled();
  });

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

    const employeeInput = screen.getByRole('combobox', { name: 'Сотрудник' });
    fireEvent.change(employeeInput, { target: { value: memberId } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить ограничение' }));
    await screen.findByText(new RegExp(`Исключить ${memberId}`));

    // A second override of the same employee is a duplicate: the action must stay disabled.
    fireEvent.change(employeeInput, { target: { value: memberId } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить ограничение' }));

    const duplicateNotice = await screen.findByText(/Дубликат: Исключить сотрудника/);
    expect(duplicateNotice).toBeDefined();
  });

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

    const employeeInput = screen.getByRole('combobox', { name: 'Сотрудник' });
    fireEvent.change(employeeInput, { target: { value: unavailable?.employee_id ?? '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить ограничение' }));

    const warning = await screen.findByText(/недоступен весь горизонт/);
    expect(warning).toBeDefined();
    // The override was not added, so the recalculation stays disabled.
    const recalculate = screen.getByRole('button', { name: 'Пересчитать' }) as HTMLButtonElement;
    expect(recalculate.disabled).toBe(true);
  });
});
