import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { mockEmployees } from '../mocks/reference';
import { useEmployeeNameFormatter } from './index';

describe('employee labels', () => {
  it('shows pseudonyms for missing names and distinguishes namesakes', () => {
    const employees = [
      { ...mockEmployees[0], display_name: 'Артём Ветров' },
      { ...mockEmployees[1], display_name: '  артём ветров  ' },
      { ...mockEmployees[2], display_name: ' ' },
      { ...mockEmployees[3], display_name: 'Анна Лескова' },
    ];
    const { result } = renderHook(() => useEmployeeNameFormatter(employees));
    expect(result.current(employees[0].employee_id)).toBe(`Артём Ветров · ${employees[0].employee_id}`);
    expect(result.current(employees[1].employee_id)).toBe(`артём ветров · ${employees[1].employee_id}`);
    expect(result.current(employees[2].employee_id)).toBe(employees[2].employee_id);
    expect(result.current(employees[3].employee_id)).toBe('Анна Лескова');
  });
});
