/**
 * Utility helpers for Employee bulk CSV / JSON parsing and template generation.
 *
 * The payload produced here is contract-shaped for PUT /api/v1/employees: unit_id,
 * internal/contractor/external_partner employment types and competency confidence/source
 * are required by the backend validator.
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

function normalizeEmploymentType(value: string): string {
  const raw = value.trim().toLowerCase();
  if (raw === 'full_time' || raw === 'fulltime') return 'internal';
  if (raw === 'part_time' || raw === 'parttime') return 'contractor';
  if (raw === 'contractor' || raw === 'external_partner') return raw;
  return 'internal';
}

function normalizeCompetencies(
  values: Array<Record<string, unknown>>,
): Array<Record<string, unknown>> {
  return values
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
# Тип занятости: internal, contractor, external_partner
# Формат компетенций: comp_id:level (уровни 1-5, разделены точкой с запятой)
employee_id,grade,role_profile_id,unit_id,employment_type,bench_flag,competencies
emp_e2e01,S,backend_engineer,unt_e2e,internal,false,lang_go:4;db_postgresql:4;api_rest:4
emp_e2e02,M,frontend_engineer,unt_e2e,internal,true,fw_react:3;lang_typescript:3
emp_e2e03,L,qa_engineer,unt_e2e,internal,false,test_playwright:4;qa_test_design:4
emp_e2e04,S,devops_engineer,unt_e2e,internal,false,cloud_yandex:4;cloud_kubernetes:4
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
  const colUnit = getCol(['unit_id', 'unit', 'отдел', 'юнит', 'team', 'подразделение', 'department']);
  const colType = getCol(['employment_type', 'type', 'тип', 'занятость']);
  const colBench = getCol(['bench_flag', 'bench', 'скамейка']);
  const colComp = getCol(['competencies', 'компетенции', 'skills']);

  const rows: ParsedEmployeeRow[] = [];

  for (let i = 1; i < lines.length; i += 1) {
    const parts = lines[i].split(',').map((p) => p.trim());
    if (parts.length === 0 || parts.every((p) => !p)) continue;

    const empId = colId >= 0 && parts[colId] ? parts[colId] : '';
    const grade = colGrade >= 0 && parts[colGrade] ? parts[colGrade].toUpperCase() : '';
    const roleId = colRole >= 0 && parts[colRole] ? parts[colRole] : '';
    const unitId = colUnit >= 0 && parts[colUnit] ? parts[colUnit] : 'unt_default';
    const empType = normalizeEmploymentType(
      colType >= 0 && parts[colType] ? parts[colType] : 'internal',
    );
    const benchRaw = colBench >= 0 && parts[colBench] ? parts[colBench].toLowerCase() : 'false';
    const bench = benchRaw === 'true' || benchRaw === '1' || benchRaw === 'да';

    const compRaw = colComp >= 0 && parts[colComp] ? parts[colComp] : '';
    const parsedComps: Array<Record<string, unknown>> = [];
    if (compRaw) {
      for (const entry of compRaw.split(';')) {
        const [cId, lvl] = entry.split(':');
        const id = cId?.trim();
        if (!id) continue;
        parsedComps.push({ competency_id: id, level: Number(lvl?.trim() || 1) });
      }
    }
    const comps = normalizeCompetencies(parsedComps);

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
      competencies: comps,
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
    ? ((jsonData as { employees?: unknown[] }).employees as unknown[])
    : [];

  return list.map((item, idx) => {
    const obj = item as Record<string, unknown>;
    const empId = String(obj.employee_id || '');
    const grade = String(obj.grade || '').toUpperCase();
    const roleId = String(obj.role_profile_id || '');
    const empType = normalizeEmploymentType(String(obj.employment_type || 'internal'));
    const comps = normalizeCompetencies(
      Array.isArray(obj.competencies) ? (obj.competencies as Array<Record<string, unknown>>) : [],
    );

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
      unit_id: String(obj.unit_id || 'unt_default'),
      employment_type: empType,
      bench_flag: Boolean(obj.bench_flag),
      competencies: comps,
    };

    return {
      key: empId || `json_${idx + 1}`,
      index: idx + 1,
      employee_id: empId,
      grade,
      role_profile_id: roleId,
      employment_type: empType,
      bench_flag: Boolean(obj.bench_flag),
      competencies_count: comps.length,
      raw,
      has_error: hasError,
      error_message: errorMsg,
    };
  });
}
