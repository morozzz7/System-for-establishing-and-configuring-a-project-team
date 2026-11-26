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
      confidence: 0.8,
      source: 'self_assessment',
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

  it('parses absences from the CSV template into the import payload', () => {
    const rows = parseCsvToEmployees(CSV_TEMPLATE_CONTENT);
    expect(rows).toHaveLength(4);
    expect(rows[0].absences_count).toBe(1);
    expect((rows[0].raw.absences as Array<Record<string, unknown>>)[0]).toMatchObject({
      employee_id: 'emp_e2e01',
      type: 'vacation',
      start_date: '2026-02-09',
      end_date: '2026-02-20',
      hours_per_day: 8,
      approved: true,
      source: 'hr_import',
    });
    expect(rows[1].absences_count).toBe(0);
    expect(rows[1].raw).not.toHaveProperty('absences');
  });

  it('preserves contract-shaped absences, assignments and capacity from JSON', () => {
    const json = {
      employees: [{
        employee_id: 'emp_j3',
        grade: 'M',
        role_profile_id: 'backend_engineer',
        weekly_hours_norm: 40,
        reserve_ratio: 0.2,
        max_utilization_ratio: 0.9,
        active: true,
        absences: [{
          absence_id: 'abs_input1',
          employee_id: 'emp_j3',
          type: 'sick_leave',
          start_date: '2026-03-02',
          end_date: '2026-03-04',
          hours_per_day: 8,
          approved: true,
        }],
        assignments: [{ project_id: 'prj_old01', role_id: 'backend_engineer', week: '2026-W10', hours: 20 }],
      }],
    };
    const [row] = parseJsonToEmployees(json);
    expect(row.has_error).toBe(false);
    expect(row.absences_count).toBe(1);
    expect(row.raw).toMatchObject({
      capacity_profile: { weekly_hours_norm: 40, reserve_ratio: 0.2, max_utilization_ratio: 0.9 },
    });
    expect(row.raw).not.toHaveProperty('active');
    const absence = (row.raw.absences as Array<Record<string, unknown>>)[0];
    expect(absence).toMatchObject({ type: 'sick_leave', source: 'hr_import', approved: true });
    expect(absence).not.toHaveProperty('absence_id');
    expect(row.raw.assignments).toEqual([{
      employee_id: 'emp_j3',
      project_id: 'prj_old01',
      role_id: 'backend_engineer',
      week: '2026-W10',
      hours: 20,
    }]);
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
