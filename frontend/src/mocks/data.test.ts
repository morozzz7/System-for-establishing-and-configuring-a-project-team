/**
 * Tests for mock data invariants.
 */
import { describe, it, expect } from 'vitest';
import { mockEmployees, mockRoles } from '../mocks/reference';
import { mockProjects, mockAssignments, mockAbsences } from '../mocks/data';
import { isValidWeek } from '../utils/week';

describe('mock data invariants', () => {
  describe('employees', () => {
    it('have valid employee IDs matching pattern', () => {
      const pattern = /^emp_[0-9]{4}$/;
      mockEmployees.forEach((emp) => {
        expect(emp.employee_id).toMatch(pattern);
      });
    });

    it('have grades in valid set', () => {
      const validGrades = new Set(['J', 'M', 'S', 'L', 'P']);
      mockEmployees.forEach((emp) => {
        expect(validGrades.has(emp.grade)).toBe(true);
      });
    });

    it('have role_profile_ids that exist in taxonomy', () => {
      const roleIds = new Set(mockRoles.map((r) => r.role_id));
      mockEmployees.forEach((emp) => {
        expect(roleIds.has(emp.role_profile_id)).toBe(true);
      });
    });

    it('have capacity profiles with valid values', () => {
      mockEmployees.forEach((emp) => {
        expect(emp.capacity_profile.weekly_hours_norm).toBeGreaterThanOrEqual(1);
        expect(emp.capacity_profile.weekly_hours_norm).toBeLessThanOrEqual(60);
        expect(emp.capacity_profile.reserve_ratio).toBeGreaterThanOrEqual(0);
        expect(emp.capacity_profile.reserve_ratio).toBeLessThanOrEqual(0.5);
        expect(emp.capacity_profile.max_utilization_ratio).toBeGreaterThanOrEqual(0.5);
        expect(emp.capacity_profile.max_utilization_ratio).toBeLessThanOrEqual(1);
      });
    });

    it('have cost coefficients in valid range', () => {
      mockEmployees.forEach((emp) => {
        expect(emp.cost_coefficient).toBeGreaterThanOrEqual(0.5);
        expect(emp.cost_coefficient).toBeLessThanOrEqual(5);
      });
    });

    it('contain at least 30 employees', () => {
      expect(mockEmployees.length).toBeGreaterThanOrEqual(30);
    });

    it('have no personal data (no emails, phones, full names)', () => {
      const emailPattern = /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b/;
      const phonePattern = /\+?\d{10,15}/;
      mockEmployees.forEach((emp) => {
        expect(emp.employee_id).not.toMatch(emailPattern);
        expect(emp.employee_id).not.toMatch(phonePattern);
        // employee_id should not contain spaces or full names
        expect(emp.employee_id).not.toMatch(/\s/);
      });
    });
  });

  describe('projects', () => {
    it('have valid project IDs', () => {
      const pattern = /^prj_[0-9]{4}$/;
      mockProjects.forEach((prj) => {
        expect(prj.project_id).toMatch(pattern);
      });
    });

    it('have valid week keys', () => {
      mockProjects.forEach((prj) => {
        expect(isValidWeek(prj.start_week)).toBe(true);
        expect(isValidWeek(prj.end_week_planned)).toBe(true);
        if (prj.end_week_forecast) {
          expect(isValidWeek(prj.end_week_forecast)).toBe(true);
        }
      });
    });

    it('have statuses in valid set', () => {
      const validStatuses = new Set(['planned', 'active', 'paused', 'done', 'cancelled']);
      mockProjects.forEach((prj) => {
        expect(validStatuses.has(prj.status)).toBe(true);
      });
    });

    it('contain at least 5 projects', () => {
      expect(mockProjects.length).toBeGreaterThanOrEqual(5);
    });
  });

  describe('assignments', () => {
    it('have valid assignment IDs', () => {
      const pattern = /^asg_[0-9]{4}$/;
      mockAssignments.forEach((asg) => {
        expect(asg.assignment_id).toMatch(pattern);
      });
    });

    it('have valid week keys', () => {
      mockAssignments.forEach((asg) => {
        expect(isValidWeek(asg.week)).toBe(true);
      });
    });

    it('have hours in valid range', () => {
      mockAssignments.forEach((asg) => {
        expect(asg.hours).toBeGreaterThanOrEqual(0);
        expect(asg.hours).toBeLessThanOrEqual(60);
      });
    });

    it('reference existing employees', () => {
      const empIds = new Set(mockEmployees.map((e) => e.employee_id));
      mockAssignments.forEach((asg) => {
        expect(empIds.has(asg.employee_id)).toBe(true);
      });
    });

    it('reference existing projects', () => {
      const prjIds = new Set(mockProjects.map((p) => p.project_id));
      mockAssignments.forEach((asg) => {
        expect(prjIds.has(asg.project_id)).toBe(true);
      });
    });

    it('contain at least 40 assignments', () => {
      expect(mockAssignments.length).toBeGreaterThanOrEqual(40);
    });
  });

  describe('absences', () => {
    it('have valid absence IDs', () => {
      const pattern = /^abs_[0-9]{4}$/;
      mockAbsences.forEach((abs) => {
        expect(abs.absence_id).toMatch(pattern);
      });
    });

    it('have valid date formats (YYYY-MM-DD)', () => {
      const datePattern = /^\d{4}-\d{2}-\d{2}$/;
      mockAbsences.forEach((abs) => {
        expect(abs.start_date).toMatch(datePattern);
        expect(abs.end_date).toMatch(datePattern);
      });
    });

    it('have types in valid set', () => {
      const validTypes = new Set([
        'vacation',
        'sick_leave',
        'training',
        'business_trip',
        'parental_leave',
        'other',
      ]);
      mockAbsences.forEach((abs) => {
        expect(validTypes.has(abs.type)).toBe(true);
      });
    });

    it('have hours_per_day in valid range', () => {
      mockAbsences.forEach((abs) => {
        expect(abs.hours_per_day).toBeGreaterThanOrEqual(0);
        expect(abs.hours_per_day).toBeLessThanOrEqual(12);
      });
    });

    it('reference existing employees', () => {
      const empIds = new Set(mockEmployees.map((e) => e.employee_id));
      mockAbsences.forEach((abs) => {
        expect(empIds.has(abs.employee_id)).toBe(true);
      });
    });

    it('contain at least 15 absences', () => {
      expect(mockAbsences.length).toBeGreaterThanOrEqual(15);
    });

    it('have no overlapping absences per employee', () => {
      // Group by employee
      const byEmployee = new Map<string, typeof mockAbsences>();
      mockAbsences.forEach((abs) => {
        if (!byEmployee.has(abs.employee_id)) {
          byEmployee.set(abs.employee_id, []);
        }
        byEmployee.get(abs.employee_id)!.push(abs);
      });

      // Check for overlaps within each employee
      byEmployee.forEach((absences) => {
        for (let i = 0; i < absences.length; i++) {
          for (let j = i + 1; j < absences.length; j++) {
            const a = absences[i];
            const b = absences[j];
            const aStart = new Date(a.start_date);
            const aEnd = new Date(a.end_date);
            const bStart = new Date(b.start_date);
            const bEnd = new Date(b.end_date);
            // Check if ranges overlap
            const overlaps = aStart <= bEnd && bStart <= aEnd;
            expect(overlaps).toBe(false);
          }
        }
      });
    });
  });
});
