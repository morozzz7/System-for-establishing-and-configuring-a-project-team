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
  display_name?: string;
  grade: string;
  role_profile_id: string;
  employment_type: string;
  bench_flag: boolean;
  competencies_count: number;
  absences_count: number;
  raw: Record<string, unknown>;
  has_error: boolean;
  error_message?: string;
}

const COMP_DEFAULT_SOURCE = 'self_assessment';
const COMP_DEFAULT_CONFIDENCE = 0.8;

const ABSENCE_TYPES = new Set([
  'vacation', 'sick_leave', 'training', 'business_trip', 'parental_leave', 'other',
]);
const ABSENCE_SOURCES = new Set(['manual', 'hr_import', 'calendar_import']);

/** Import payloads use the contract shapes: no ids on nested rows, no extra keys (DisallowUnknownFields). */
function normalizeAbsences(
  employeeId: string,
  values: unknown,
): Array<Record<string, unknown>> {
  if (!Array.isArray(values)) return [];
  return values.flatMap((value) => {
    if (typeof value !== 'object' || value === null) return [];
    const a = value as Record<string, unknown>;
    const type = String(a.type ?? 'other');
    const startDate = String(a.start_date ?? '');
    const endDate = String(a.end_date ?? '');
    if (!ABSENCE_TYPES.has(type) || !startDate || !endDate) return [];
    const source = String(a.source ?? 'hr_import');
    return [{
      employee_id: String(a.employee_id ?? employeeId),
      type,
      start_date: startDate,
      end_date: endDate,
      hours_per_day: Number(a.hours_per_day ?? 8),
      approved: a.approved !== false,
      source: ABSENCE_SOURCES.has(source) ? source : 'hr_import',
    }];
  });
}

function normalizeAssignments(employeeId: string, values: unknown): Array<Record<string, unknown>> {
  if (!Array.isArray(values)) return [];
  return values.flatMap((value) => {
    if (typeof value !== 'object' || value === null) return [];
    const a = value as Record<string, unknown>;
    return [{
      employee_id: String(a.employee_id ?? employeeId),
      project_id: String(a.project_id ?? ''),
      role_id: String(a.role_id ?? ''),
      week: String(a.week ?? ''),
      hours: Number(a.hours ?? 0),
    }];
  });
}

function normalizeCapacityProfile(obj: Record<string, unknown>): Record<string, number> | undefined {
  const nested = obj.capacity_profile;
  const source = (typeof nested === 'object' && nested !== null ? nested : obj) as Record<string, unknown>;
  const norm = Number(source.weekly_hours_norm);
  if (!Number.isFinite(norm) || norm <= 0) return undefined;
  return {
    weekly_hours_norm: norm,
    reserve_ratio: Number(source.reserve_ratio ?? 0),
    max_utilization_ratio: Number(source.max_utilization_ratio ?? 1),
  };
}

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
# Отсутствия (необязательно): type:start_date:end_date [;type:start_date:end_date]
# Типы отсутствий: vacation, sick_leave, training, business_trip, parental_leave, other
employee_id,grade,role_profile_id,unit_id,employment_type,bench_flag,competencies,absences
emp_e2e01,S,backend_engineer,unt_e2e,internal,false,lang_go:4;db_postgresql:4;api_rest:4,vacation:2026-02-09:2026-02-20
emp_e2e02,M,frontend_engineer,unt_e2e,internal,true,fw_react:3;lang_typescript:3,
emp_e2e03,L,qa_engineer,unt_e2e,internal,false,test_playwright:4;qa_test_design:4,sick_leave:2026-03-02:2026-03-04
emp_e2e04,S,devops_engineer,unt_e2e,internal,false,cloud_yandex:4;cloud_kubernetes:4,
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
  const colName = getCol(['display_name', 'имя']);
  const colGrade = getCol(['grade', 'грейд']);
  const colRole = getCol(['role_profile_id', 'role', 'роль']);
  const colUnit = getCol(['unit_id', 'unit', 'отдел', 'юнит', 'team', 'подразделение', 'department']);
  const colType = getCol(['employment_type', 'type', 'тип', 'занятость']);
  const colBench = getCol(['bench_flag', 'bench', 'скамейка']);
  const colComp = getCol(['competencies', 'компетенции', 'skills']);
  const colAbs = getCol(['absences', 'отсутствия', 'absence']);

  const rows: ParsedEmployeeRow[] = [];

  for (let i = 1; i < lines.length; i += 1) {
    const parts = lines[i].split(',').map((p) => p.trim());
    if (parts.length === 0 || parts.every((p) => !p)) continue;

    const empId = colId >= 0 && parts[colId] ? parts[colId] : '';
    const displayName = colName >= 0 ? parts[colName] : undefined;
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

    const absRaw = colAbs >= 0 && parts[colAbs] ? parts[colAbs] : '';
    const parsedAbsences: Array<Record<string, unknown>> = [];
    if (absRaw) {
      for (const entry of absRaw.split(';')) {
        const [type, startDate, endDate] = entry.split(':').map((part) => part?.trim());
        if (!type || !startDate || !endDate) continue;
        parsedAbsences.push({ type, start_date: startDate, end_date: endDate, hours_per_day: 8 });
      }
    }
    const absences = normalizeAbsences(empId, parsedAbsences);

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
      ...(displayName !== undefined ? { display_name: displayName } : {}),
      grade,
      role_profile_id: roleId,
      unit_id: unitId,
      employment_type: empType,
      bench_flag: bench,
      competencies: comps,
      ...(absences.length > 0 ? { absences } : {}),
    };

    rows.push({
      key: empId || `row_${i}`,
      index: i,
      employee_id: empId,
      ...(displayName !== undefined ? { display_name: displayName } : {}),
      grade,
      role_profile_id: roleId,
      employment_type: empType,
      bench_flag: bench,
      competencies_count: comps.length,
      absences_count: absences.length,
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
    const displayName = typeof obj.display_name === 'string' ? obj.display_name.trim() : undefined;
    const grade = String(obj.grade || '').toUpperCase();
    const roleId = String(obj.role_profile_id || '');
    const empType = normalizeEmploymentType(String(obj.employment_type || 'internal'));
    const comps = normalizeCompetencies(
      Array.isArray(obj.competencies) ? (obj.competencies as Array<Record<string, unknown>>) : [],
    );
    const absences = normalizeAbsences(empId, obj.absences);
    const assignments = normalizeAssignments(empId, obj.assignments);
    const capacityProfile = normalizeCapacityProfile(obj);

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
      ...(displayName !== undefined ? { display_name: displayName } : {}),
      grade,
      role_profile_id: roleId,
      unit_id: String(obj.unit_id || 'unt_default'),
      employment_type: empType,
      bench_flag: Boolean(obj.bench_flag),
      competencies: comps,
      ...(capacityProfile ? { capacity_profile: capacityProfile } : {}),
      ...(assignments.length > 0 ? { assignments } : {}),
      ...(absences.length > 0 ? { absences } : {}),
    };

    return {
      key: empId || `json_${idx + 1}`,
      index: idx + 1,
      employee_id: empId,
      ...(displayName !== undefined ? { display_name: displayName } : {}),
      grade,
      role_profile_id: roleId,
      employment_type: empType,
      bench_flag: Boolean(obj.bench_flag),
      competencies_count: comps.length,
      absences_count: absences.length,
      raw,
      has_error: hasError,
      error_message: errorMsg,
    };
  });
}
