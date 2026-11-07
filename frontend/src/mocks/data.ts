/**
 * Mock data for projects, assignments, absences, and employee competencies.
 */
import type {
  Project,
  Assignment,
  Absence,
  EmployeeCompetency,
  CapacityWeek,
} from '../types';

// ──── Projects (5 with diverse statuses) ────

export const mockProjects: Project[] = [
  {
    project_id: 'prj_0001',
    code: 'CRM-2026',
    name: 'CRM rework',
    status: 'active',
    priority: 2,
    criticality: 'high',
    start_week: '2025-W40',
    end_week_planned: '2026-W20',
    replaces_other_projects: false,
  },
  {
    project_id: 'prj_0002',
    code: 'PORTAL-2026',
    name: 'Portal migration to Go',
    status: 'planned',
    priority: 1,
    criticality: 'critical',
    start_week: '2026-W10',
    end_week_planned: '2026-W24',
    replaces_other_projects: false,
  },
  {
    project_id: 'prj_0003',
    code: 'ML-PIPE-2025',
    name: 'ML Pipeline v2',
    status: 'active',
    priority: 2,
    criticality: 'normal',
    start_week: '2025-W30',
    end_week_planned: '2026-W15',
    replaces_other_projects: false,
  },
  {
    project_id: 'prj_0004',
    code: 'MOBILE-2025',
    name: 'Mobile app redesign',
    status: 'done',
    priority: 3,
    criticality: 'normal',
    start_week: '2025-W20',
    end_week_planned: '2026-W05',
    end_week_forecast: '2026-W06',
    replaces_other_projects: false,
  },
  {
    project_id: 'prj_0005',
    code: 'SEC-AUDIT-2026',
    name: 'Security compliance audit',
    status: 'active',
    priority: 1,
    criticality: 'critical',
    start_week: '2026-W01',
    end_week_planned: '2026-W30',
    replaces_other_projects: false,
  },
];

// ──── Assignments (~40 entries across employees and weeks) ────

export const mockAssignments: Assignment[] = [
  // prj_0001 CRM-2026 (active)
  { assignment_id: 'asg_0001', project_id: 'prj_0001', employee_id: 'emp_0001', role_id: 'backend_engineer', week: '2026-W07', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0002', project_id: 'prj_0001', employee_id: 'emp_0001', role_id: 'backend_engineer', week: '2026-W08', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0003', project_id: 'prj_0001', employee_id: 'emp_0002', role_id: 'backend_engineer', week: '2026-W07', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0004', project_id: 'prj_0001', employee_id: 'emp_0004', role_id: 'frontend_engineer', week: '2026-W07', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0005', project_id: 'prj_0001', employee_id: 'emp_0004', role_id: 'frontend_engineer', week: '2026-W08', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0006', project_id: 'prj_0001', employee_id: 'emp_0006', role_id: 'qa_engineer', week: '2026-W07', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0007', project_id: 'prj_0001', employee_id: 'emp_0013', role_id: 'business_analyst', week: '2026-W07', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0008', project_id: 'prj_0001', employee_id: 'emp_0016', role_id: 'project_manager', week: '2026-W07', hours: 20, source: 'plan' },

  // prj_0003 ML-PIPE-2025 (active)
  { assignment_id: 'asg_0009', project_id: 'prj_0003', employee_id: 'emp_0010', role_id: 'data_engineer', week: '2026-W07', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0010', project_id: 'prj_0003', employee_id: 'emp_0010', role_id: 'data_engineer', week: '2026-W08', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0011', project_id: 'prj_0003', employee_id: 'emp_0012', role_id: 'ml_engineer', week: '2026-W07', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0012', project_id: 'prj_0003', employee_id: 'emp_0012', role_id: 'ml_engineer', week: '2026-W08', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0013', project_id: 'prj_0003', employee_id: 'emp_0026', role_id: 'ml_engineer', week: '2026-W07', hours: 20, source: 'plan' },

  // prj_0005 SEC-AUDIT-2026 (active)
  { assignment_id: 'asg_0014', project_id: 'prj_0005', employee_id: 'emp_0021', role_id: 'security_engineer', week: '2026-W07', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0015', project_id: 'prj_0005', employee_id: 'emp_0021', role_id: 'security_engineer', week: '2026-W08', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0016', project_id: 'prj_0005', employee_id: 'emp_0008', role_id: 'devops_engineer', week: '2026-W07', hours: 16, source: 'plan' },

  // Additional scattered assignments for various employees
  { assignment_id: 'asg_0017', project_id: 'prj_0001', employee_id: 'emp_0005', role_id: 'frontend_engineer', week: '2026-W07', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0018', project_id: 'prj_0001', employee_id: 'emp_0009', role_id: 'devops_engineer', week: '2026-W07', hours: 12, source: 'plan' },
  { assignment_id: 'asg_0019', project_id: 'prj_0003', employee_id: 'emp_0011', role_id: 'data_engineer', week: '2026-W07', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0020', project_id: 'prj_0001', employee_id: 'emp_0014', role_id: 'business_analyst', week: '2026-W07', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0021', project_id: 'prj_0005', employee_id: 'emp_0015', role_id: 'system_analyst', week: '2026-W07', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0022', project_id: 'prj_0001', employee_id: 'emp_0019', role_id: 'ui_designer', week: '2026-W07', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0023', project_id: 'prj_0001', employee_id: 'emp_0018', role_id: 'product_manager', week: '2026-W07', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0024', project_id: 'prj_0003', employee_id: 'emp_0017', role_id: 'project_manager', week: '2026-W07', hours: 12, source: 'plan' },

  // Second week assignments
  { assignment_id: 'asg_0025', project_id: 'prj_0001', employee_id: 'emp_0002', role_id: 'backend_engineer', week: '2026-W08', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0026', project_id: 'prj_0001', employee_id: 'emp_0006', role_id: 'qa_engineer', week: '2026-W08', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0027', project_id: 'prj_0001', employee_id: 'emp_0013', role_id: 'business_analyst', week: '2026-W08', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0028', project_id: 'prj_0001', employee_id: 'emp_0016', role_id: 'project_manager', week: '2026-W08', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0029', project_id: 'prj_0003', employee_id: 'emp_0010', role_id: 'data_engineer', week: '2026-W09', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0030', project_id: 'prj_0003', employee_id: 'emp_0012', role_id: 'ml_engineer', week: '2026-W09', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0031', project_id: 'prj_0005', employee_id: 'emp_0021', role_id: 'security_engineer', week: '2026-W09', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0032', project_id: 'prj_0001', employee_id: 'emp_0001', role_id: 'backend_engineer', week: '2026-W09', hours: 32, source: 'plan' },
  { assignment_id: 'asg_0033', project_id: 'prj_0001', employee_id: 'emp_0004', role_id: 'frontend_engineer', week: '2026-W09', hours: 28, source: 'plan' },
  { assignment_id: 'asg_0034', project_id: 'prj_0003', employee_id: 'emp_0026', role_id: 'ml_engineer', week: '2026-W08', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0035', project_id: 'prj_0005', employee_id: 'emp_0008', role_id: 'devops_engineer', week: '2026-W08', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0036', project_id: 'prj_0001', employee_id: 'emp_0005', role_id: 'frontend_engineer', week: '2026-W08', hours: 16, source: 'plan' },
  { assignment_id: 'asg_0037', project_id: 'prj_0001', employee_id: 'emp_0009', role_id: 'devops_engineer', week: '2026-W08', hours: 12, source: 'plan' },
  { assignment_id: 'asg_0038', project_id: 'prj_0003', employee_id: 'emp_0011', role_id: 'data_engineer', week: '2026-W08', hours: 24, source: 'plan' },
  { assignment_id: 'asg_0039', project_id: 'prj_0001', employee_id: 'emp_0014', role_id: 'business_analyst', week: '2026-W08', hours: 20, source: 'plan' },
  { assignment_id: 'asg_0040', project_id: 'prj_0005', employee_id: 'emp_0015', role_id: 'system_analyst', week: '2026-W08', hours: 16, source: 'plan' },
];

// ──── Absences (~15 entries) ────

export const mockAbsences: Absence[] = [
  { absence_id: 'abs_0001', employee_id: 'emp_0001', type: 'vacation', start_date: '2026-02-09', end_date: '2026-02-20', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0002', employee_id: 'emp_0004', type: 'vacation', start_date: '2026-03-02', end_date: '2026-03-13', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0003', employee_id: 'emp_0008', type: 'business_trip', start_date: '2026-02-16', end_date: '2026-02-18', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0004', employee_id: 'emp_0010', type: 'training', start_date: '2026-02-23', end_date: '2026-02-25', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0005', employee_id: 'emp_0006', type: 'sick_leave', start_date: '2026-02-10', end_date: '2026-02-12', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0006', employee_id: 'emp_0012', type: 'vacation', start_date: '2026-03-16', end_date: '2026-03-27', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0007', employee_id: 'emp_0014', type: 'business_trip', start_date: '2026-02-24', end_date: '2026-02-25', hours_per_day: 4, approved: true, source: 'manual' },
  { absence_id: 'abs_0008', employee_id: 'emp_0016', type: 'vacation', start_date: '2026-04-06', end_date: '2026-04-17', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0009', employee_id: 'emp_0002', type: 'sick_leave', start_date: '2026-02-17', end_date: '2026-02-19', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0010', employee_id: 'emp_0019', type: 'other', start_date: '2026-02-12', end_date: '2026-02-12', hours_per_day: 4, approved: true, source: 'manual' },
  { absence_id: 'abs_0011', employee_id: 'emp_0021', type: 'vacation', start_date: '2026-03-09', end_date: '2026-03-20', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0012', employee_id: 'emp_0005', type: 'training', start_date: '2026-02-19', end_date: '2026-02-20', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0013', employee_id: 'emp_0025', type: 'vacation', start_date: '2026-02-02', end_date: '2026-02-13', hours_per_day: 8, approved: true, source: 'hr_import' },
  { absence_id: 'abs_0014', employee_id: 'emp_0027', type: 'business_trip', start_date: '2026-03-04', end_date: '2026-03-06', hours_per_day: 8, approved: true, source: 'manual' },
  { absence_id: 'abs_0015', employee_id: 'emp_0018', type: 'other', start_date: '2026-02-14', end_date: '2026-02-14', hours_per_day: 4, approved: true, source: 'manual' },
];

// ──── Employee competencies (for emp_0001, emp_0004, emp_0010 as examples) ────

export const mockEmployeeCompetencies: Record<string, EmployeeCompetency[]> = {
  emp_0001: [
    { competency_id: 'lang_go', level: 4, confidence: 0.9, last_used: '2026-10-20', source: 'project_history' },
    { competency_id: 'db_postgresql', level: 4, confidence: 0.85, last_used: '2026-09-30', source: 'manager_review' },
    { competency_id: 'api_rest', level: 5, confidence: 0.95, last_used: '2026-10-15', source: 'project_history' },
    { competency_id: 'lang_java', level: 2, confidence: 0.6, last_used: '2024-06-01', source: 'self_assessment' },
  ],
  emp_0002: [
    { competency_id: 'lang_go', level: 3, confidence: 0.8, last_used: '2026-10-10', source: 'project_history' },
    { competency_id: 'db_postgresql', level: 3, confidence: 0.75, last_used: '2026-09-15', source: 'manager_review' },
    { competency_id: 'api_rest', level: 3, confidence: 0.8, last_used: '2026-10-05', source: 'project_history' },
  ],
  emp_0004: [
    { competency_id: 'lang_typescript', level: 5, confidence: 0.95, last_used: '2026-10-22', source: 'project_history' },
    { competency_id: 'fw_react', level: 4, confidence: 0.9, last_used: '2026-10-20', source: 'project_history' },
    { competency_id: 'api_rest', level: 3, confidence: 0.8, last_used: '2026-09-25', source: 'manager_review' },
  ],
  emp_0010: [
    { competency_id: 'lang_python', level: 4, confidence: 0.9, last_used: '2026-10-18', source: 'project_history' },
    { competency_id: 'lang_sql', level: 4, confidence: 0.85, last_used: '2026-10-10', source: 'project_history' },
    { competency_id: 'db_postgresql', level: 3, confidence: 0.75, last_used: '2026-09-20', source: 'manager_review' },
    { competency_id: 'db_clickhouse', level: 4, confidence: 0.85, last_used: '2026-10-15', source: 'project_history' },
  ],
  emp_0012: [
    { competency_id: 'lang_python', level: 5, confidence: 0.95, last_used: '2026-10-22', source: 'project_history' },
    { competency_id: 'ml_pytorch', level: 4, confidence: 0.9, last_used: '2026-10-20', source: 'project_history' },
    { competency_id: 'lang_sql', level: 3, confidence: 0.7, last_used: '2026-08-15', source: 'self_assessment' },
  ],
  emp_0021: [
    { competency_id: 'sec_appsec', level: 4, confidence: 0.85, last_used: '2026-10-15', source: 'project_history' },
    { competency_id: 'sec_crypto', level: 3, confidence: 0.8, last_used: '2026-09-30', source: 'manager_review' },
  ],
  emp_0025: [
    { competency_id: 'lang_go', level: 5, confidence: 0.95, last_used: '2026-10-22', source: 'project_history' },
    { competency_id: 'db_postgresql', level: 5, confidence: 0.9, last_used: '2026-10-20', source: 'project_history' },
    { competency_id: 'api_rest', level: 5, confidence: 0.95, last_used: '2026-10-22', source: 'project_history' },
    { competency_id: 'lang_java', level: 3, confidence: 0.7, last_used: '2025-03-01', source: 'self_assessment' },
  ],
};

// ──── Employee capacity (for emp_0001, weeks 2026-W07..W10) ────

export const mockCapacityWeeks: CapacityWeek[] = [
  { employee_id: 'emp_0001', week: '2026-W07', working_days: 5, norm_hours: 40, reserved_hours: 8, assigned_hours: 32, absence_hours: 0, free_hours: 0, load_ratio: 0.8, unavailable: false },
  { employee_id: 'emp_0001', week: '2026-W08', working_days: 5, norm_hours: 40, reserved_hours: 8, assigned_hours: 32, absence_hours: 0, free_hours: 0, load_ratio: 0.8, unavailable: false },
  { employee_id: 'emp_0001', week: '2026-W09', working_days: 5, norm_hours: 40, reserved_hours: 8, assigned_hours: 32, absence_hours: 0, free_hours: 0, load_ratio: 0.8, unavailable: false },
  { employee_id: 'emp_0001', week: '2026-W06', working_days: 5, norm_hours: 40, reserved_hours: 8, assigned_hours: 0, absence_hours: 40, free_hours: 0, load_ratio: 0, unavailable: true },
  { employee_id: 'emp_0001', week: '2026-W05', working_days: 5, norm_hours: 40, reserved_hours: 8, assigned_hours: 0, absence_hours: 40, free_hours: 0, load_ratio: 0, unavailable: true },
];
