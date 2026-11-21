/**
 * Utility helpers for Employee bulk CSV / JSON parsing and template generation.
 */

export interface ParsedEmployeeRow {
  key: string;
  index: number;
  employee_id: string;
  grade: string;
  role_profile_id: string;
  employment_type: string;
  bench_flag: boolean;
  competencies_count: number;
  raw: Record<string, unknown>;
  has_error: boolean;
  error_message?: string;
}

const COMP_DEFAULT_SOURCE = 'self_assessment';
const COMP_DEFAULT_CONFIDENCE = 0.8;

function normalizeEmploymentType(raw: string): string {
  const value = raw.trim().toLowerCase();
  if (value === 'full_time' || value === 'fulltime') return 'internal';
  if (value === 'part_time' || value === 'parttime') return 'contractor';
  return value || 'internal';
}

function normalizeCompetencies(comps: Array<Record<string, unknown>>): Array<Record<string, unknown>> {
  return comps
    .map((c) => ({
      competency_id: String(c.competency_id ?? ''),
      level: Number(c.level ?? 1),
      confidence: typeof c.confidence === 'number' ? c.confidence : COMP_DEFAULT_CONFIDENCE,
      source: typeof c.source === 'string' && c.source ? c.source : COMP_DEFAULT_SOURCE,
    }))
    .filter((c) => c.competency_id);
}

export const CSV_TEMPLATE_CONTENT = `# Шаблон импорта сотрудников
# Допустимые грейды: J (Junior), M (Middle), S (Senior), L (Lead), P (Principal)
# Формат компетенций: comp_id:level (уровни 1-5, разделены точкой с запятой)
employee_id,grade,role_profile_id,employment_type,bench_flag,competencies,unit_id
emp_0101,S,backend_dev,internal,false,lang_go:4;db_postgres:3;cloud_docker:3,unt_core
emp_0102,M,frontend_dev,internal,true,fe_react:3;lang_typescript:3;ui_figma:2,unt_web
emp_0103,L,team_lead,internal,false,arch_solution:4;lang_python:4;db_postgres:4,unt_core
emp_0104,J,qa_engineer,internal,true,test_manual:2;test_automation:2,unt_qa
emp_0105,S,devops,internal,false,cloud_k8s:4;cloud_docker:4;linux_kernel:3,unt_infra
`;

export function downloadCsvTemplate(): void {
  const blob = new Blob([CSV_TEMPLATE_CONTENT], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.setAttribute('href', url);
  link.setAttribute('download', 'employees_template.csv');
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export function parseCsvToEmployees(csvText: string): ParsedEmployeeRow[] {
  const lines = csvText.split(/\r?\n/).map((l) => l.trim()).filter((l) => l && !l.startsWith('#'));
  if (lines.length < 2) return [];

  const headerLine = lines[0];
  const headers = headerLine.split(',').map((h) => h.trim().toLowerCase());

  const getCol = (names: string[]): number => headers.findIndex((h) => names.includes(h));
  const colId = getCol(['employee_id', 'id', 'ид']);
  const colGrade = getCol(['grade', 'грейд']);
  const colRole = getCol(['role_profile_id', 'role', 'роль']);
  const colType = getCol(['employment_type', 'type', 'тип', 'занятость']);
  const colBench = getCol(['bench_flag', 'bench', 'скамейка']);
  const colComp = getCol(['competencies', 'компетенции', 'skills']);
  const colUnit = getCol(['unit_id', 'unit', 'отдел', 'юнит', 'team', 'подразделение', 'department']);

  const rows: ParsedEmployeeRow[] = [];

  for (let i = 1; i < lines.length; i++) {
    const parts = lines[i].split(',').map((p) => p.trim());
    if (parts.length === 0 || parts.every((p) => !p)) continue;

    const empId = colId >= 0 && parts[colId] ? parts[colId] : '';
    const grade = colGrade >= 0 && parts[colGrade] ? parts[colGrade].toUpperCase() : '';
    const roleId = colRole >= 0 && parts[colRole] ? parts[colRole] : '';
    const empType = colType >= 0 && parts[colType] ? normalizeEmploymentType(parts[colType]) : 'internal';
    const benchRaw = colBench >= 0 && parts[colBench] ? parts[colBench].toLowerCase() : 'false';
    const bench = benchRaw === 'true' || benchRaw === '1' || benchRaw === 'да';

    const unitId = colUnit >= 0 && parts[colUnit] ? parts[colUnit] : '';
    const compRaw = colComp >= 0 && parts[colComp] ? parts[colComp] : '';
    const comps: Array<Record<string, unknown>> = [];
    if (compRaw) {
      for (const entry of compRaw.split(';')) {
        const [cId, lvl] = entry.split(':');
        const id = cId?.trim();
        if (!id) continue;
        comps.push({
          competency_id: id,
          level: Number(lvl?.trim() || 1),
          confidence: COMP_DEFAULT_CONFIDENCE,
          source: COMP_DEFAULT_SOURCE,
        });
      }
    }

    let hasError = false;
    let errorMsg = '';

    if (!empId) {
      hasError = true;
      errorMsg = 'Отсутствует ID сотрудника';
    } else if (!/^[a-zA-Z0-9_-]+$/.test(empId)) {
      hasError = true;
      errorMsg = 'Недопустимые символы в ID';
    } else if (!['J', 'M', 'S', 'L', 'P'].includes(grade)) {
      hasError = true;
      errorMsg = `Недопустимый грейд: ${grade || 'пусто'}`;
    } else if (!roleId) {
      hasError = true;
      errorMsg = 'Не указана роль';
    }

    const raw: Record<string, unknown> = {
      employee_id: empId,
      grade,
      role_profile_id: roleId,
      unit_id: unitId,
      employment_type: empType,
      bench_flag: bench,
      competencies: normalizeCompetencies(comps),
    };

    rows.push({
      key: empId || `row_${i}`,
      index: i,
      employee_id: empId,
      grade,
      role_profile_id: roleId,
      employment_type: empType,
      bench_flag: bench,
      competencies_count: comps.length,
      raw,
      has_error: hasError,
      error_message: errorMsg,
    });
  }

  return rows;
}

export function parseJsonToEmployees(jsonData: unknown): ParsedEmployeeRow[] {
  if (typeof jsonData !== 'object' || jsonData === null) return [];
  const list = Array.isArray(jsonData)
    ? jsonData
    : Array.isArray((jsonData as { employees?: unknown[] }).employees)
    ? (jsonData as { employees: unknown[] }).employees
    : [];

  return list.map((item, idx) => {
    const obj = item as Record<string, unknown>;
    const empId = String(obj.employee_id || '');
    const grade = String(obj.grade || '').toUpperCase();
    const roleId = String(obj.role_profile_id || '');
    const empType = normalizeEmploymentType(String(obj.employment_type || ''));
    const bench = Boolean(obj.bench_flag);
    const comps = normalizeCompetencies(Array.isArray(obj.competencies) ? (obj.competencies as Array<Record<string, unknown>>) : []);

    let hasError = false;
    let errorMsg = '';
    if (!empId) {
      hasError = true;
      errorMsg = 'Отсутствует employee_id';
    } else if (!['J', 'M', 'S', 'L', 'P'].includes(grade)) {
      hasError = true;
      errorMsg = `Некорректный грейд "${grade}"`;
    } else if (!roleId) {
      hasError = true;
      errorMsg = 'Отсутствует role_profile_id';
    }

    const raw: Record<string, unknown> = {
      employee_id: empId,
      grade,
      role_profile_id: roleId,
      unit_id: String(obj.unit_id || ''),
      employment_type: empType,
      bench_flag: bench,
      competencies: comps,
    };

    return {
      key: empId || `json_${idx + 1}`,
      index: idx + 1,
      employee_id: empId,
      grade,
      role_profile_id: roleId,
      employment_type: empType,
      bench_flag: bench,
      competencies_count: comps.length,
      raw,
      has_error: hasError,
      error_message: errorMsg,
    };
  });
}
