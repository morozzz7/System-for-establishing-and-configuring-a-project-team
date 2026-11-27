import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import ProjectDetailPage from './ProjectDetailPage';
import type { Absence, Assignment, Project, Role } from '../types';

const project: Project = {
  project_id: 'prj_portal01',
  code: 'PRJ-PORTAL01',
  name: 'Миграция портала',
  status: 'planned',
  priority: 3,
  criticality: 'high',
  start_week: '2026-W10',
  end_week_planned: '2026-W20',
  replaces_other_projects: false,
};

const assignments: Assignment[] = [
  {
    assignment_id: 'asg_0001',
    project_id: project.project_id,
    employee_id: 'emp_dev01',
    role_id: 'backend_engineer',
    week: '2026-W10',
    hours: 20,
    source: 'plan',
  },
  {
    assignment_id: 'asg_0002',
    project_id: 'prj_other01',
    employee_id: 'emp_other1',
    role_id: 'qa_engineer',
    week: '2026-W10',
    hours: 8,
    source: 'plan',
  },
];

const absences: Absence[] = [
  {
    absence_id: 'abs_0001',
    employee_id: 'emp_dev01',
    type: 'vacation',
    start_date: '2026-11-02',
    end_date: '2026-11-06',
    hours_per_day: 8,
    approved: true,
    source: 'hr_import',
  },
];

const roles: Role[] = [
  {
    role_id: 'backend_engineer',
    name_en: 'Backend Engineer',
    name_ru: 'Бэкенд-разработчик',
    family: 'engineering',
    synonyms: [],
    core_competencies: [],
    typical_grades: ['M'],
  },
];

vi.mock('../hooks', () => ({
  useProject: (projectId: string) => ({
    data: projectId === project.project_id ? project : null,
    loading: false,
    error: null,
  }),
  useAssignments: (projectId?: string) => ({
    data: assignments.filter((item) => !projectId || item.project_id === projectId),
    loading: false,
    error: null,
  }),
  useEmployeeNames: () => (id: string) => id === 'emp_dev01' ? 'Анна Лескова' : 'Максим Озеров',
  useAbsences: () => ({ data: absences, loading: false, error: null }),
  useRoles: () => ({ data: roles, loading: false, error: null }),
}));

const { deleteProject } = vi.hoisted(() => ({
  deleteProject: vi.fn(async (_id: string) => undefined),
}));
vi.mock('../api/client', () => ({ deleteProject }));

describe('ProjectDetailPage', () => {
  it('shows the project card, only its own assignments and absences of assigned staff', async () => {
    render(
      <ConfigProvider locale={ruRU}>
        <MemoryRouter initialEntries={['/projects/prj_portal01']}>
          <Routes>
            <Route path="/projects/:projectId" element={<ProjectDetailPage />} />
          </Routes>
        </MemoryRouter>
      </ConfigProvider>,
    );

    expect(await screen.findByText('Миграция портала')).toBeDefined();
    expect(screen.getByText('PRJ-PORTAL01')).toBeDefined();
    expect(screen.getAllByText('Анна Лескова').length).toBeGreaterThan(0);
    expect(screen.getByText('Бэкенд-разработчик')).toBeDefined();
    expect(screen.getAllByText('20 ч').length).toBeGreaterThan(0);
    expect(screen.getByText('Отпуск')).toBeDefined();
    expect(screen.queryByText('Максим Озеров')).toBeNull();
  });

  it('deletes the project from the detail page and returns to the list', async () => {
    deleteProject.mockClear();
    render(
      <ConfigProvider locale={ruRU}>
        <MemoryRouter initialEntries={['/projects/prj_portal01']}>
          <Routes>
            <Route path="/projects" element={<div>Список проектов</div>} />
            <Route path="/projects/:projectId" element={<ProjectDetailPage />} />
          </Routes>
        </MemoryRouter>
      </ConfigProvider>,
    );

    fireEvent.click(await screen.findByTestId('delete-project'));
    fireEvent.click(await screen.findByRole('button', { name: 'Удалить' }));

    await waitFor(() => expect(deleteProject).toHaveBeenCalledWith('prj_portal01'));
    expect(await screen.findByText('Список проектов')).toBeDefined();
  });
});
