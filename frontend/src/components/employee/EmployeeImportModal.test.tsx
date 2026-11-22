import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import EmployeeImportModal from './EmployeeImportModal';
import {
  parseCsvToEmployees,
  parseJsonToEmployees,
  CSV_TEMPLATE_CONTENT,
} from '../../utils/csvImport';

describe('EmployeeImportModal logic', () => {
  it('has valid CSV template content', () => {
    expect(CSV_TEMPLATE_CONTENT).toContain(
      'employee_id,grade,role_profile_id,unit_id,employment_type,bench_flag,competencies',
    );
    expect(CSV_TEMPLATE_CONTENT).toContain('emp_e2e01,S,backend_engineer');
  });

  it('parses valid CSV correctly', () => {
    const csv = `employee_id,grade,role_profile_id,employment_type,bench_flag,competencies
emp_test1,S,backend_dev,full_time,true,lang_go:4;db_postgres:3
emp_test2,M,frontend_dev,part_time,false,fe_react:3
`;
    const rows = parseCsvToEmployees(csv);
    expect(rows.length).toBe(2);
    expect(rows[0].employee_id).toBe('emp_test1');
    expect(rows[0].grade).toBe('S');
    expect(rows[0].role_profile_id).toBe('backend_dev');
    expect(rows[0].bench_flag).toBe(true);
    expect(rows[0].has_error).toBe(false);
    expect(rows[0].competencies_count).toBe(2);
    expect(rows[0].raw.unit_id).toBe('unt_default');
    expect(rows[0].raw.employment_type).toBe('internal');
    expect((rows[0].raw.competencies as Array<Record<string, unknown>>)[0]).toMatchObject({
      competency_id: 'lang_go',
      level: 4,
      confidence: 0.9,
      source: 'manager_review',
    });

    expect(rows[1].employee_id).toBe('emp_test2');
    expect(rows[1].grade).toBe('M');
    expect(rows[1].bench_flag).toBe(false);
  });

  it('detects errors in invalid CSV rows', () => {
    const csv = `employee_id,grade,role_profile_id
emp_bad1,INVALID_GRADE,backend_dev
,S,frontend_dev
`;
    const rows = parseCsvToEmployees(csv);
    expect(rows.length).toBe(2);
    expect(rows[0].has_error).toBe(true);
    expect(rows[0].error_message).toContain('Недопустимый грейд');

    expect(rows[1].has_error).toBe(true);
    expect(rows[1].error_message).toContain('Отсутствует ID');
  });

  it('parses valid JSON data', () => {
    const json = {
      employees: [
        { employee_id: 'emp_j1', grade: 'L', role_profile_id: 'team_lead' },
        { employee_id: 'emp_j2', grade: 'J', role_profile_id: 'qa_engineer' },
      ],
    };
    const rows = parseJsonToEmployees(json);
    expect(rows.length).toBe(2);
    expect(rows[0].employee_id).toBe('emp_j1');
    expect(rows[0].grade).toBe('L');
    expect(rows[0].has_error).toBe(false);
    expect(rows[0].raw.unit_id).toBe('unt_default');
    expect(rows[0].raw.employment_type).toBe('internal');
    expect(rows[1].grade).toBe('J');
  });

  it('renders modal when open=true and handles cancel', () => {
    const handleClose = vi.fn();
    render(<EmployeeImportModal open={true} onClose={handleClose} onSuccess={vi.fn()} />);

    expect(screen.getByText(/Массовая загрузка сотрудников/i)).toBeDefined();
    expect(screen.getByText(/Перетащите JSON или CSV/i)).toBeDefined();
    expect(screen.getByText('Шаблон CSV')).toBeDefined();

    const cancelBtn = screen.getByText('Отмена');
    fireEvent.click(cancelBtn);
    expect(handleClose).toHaveBeenCalled();
  });
});
