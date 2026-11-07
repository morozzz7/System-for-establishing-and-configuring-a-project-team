/**
 * Main application component with React Router v6 routing.
 */
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import AppLayout from './app/Layout';
import DashboardPage from './pages/DashboardPage';
import EmployeesPage from './pages/EmployeesPage';
import ProjectsPage from './pages/ProjectsPage';
import AssignmentsPage from './pages/AssignmentsPage';
import AbsencesPage from './pages/AbsencesPage';
import {
  RolesPage,
  CompetenciesPage,
  GradesPage,
  WeightPresetsPage,
  PlanningParamsPage,
} from './pages/ReferencePages';

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<AppLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="employees" element={<EmployeesPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="assignments" element={<AssignmentsPage />} />
          <Route path="absences" element={<AbsencesPage />} />
          <Route path="reference/roles" element={<RolesPage />} />
          <Route path="reference/competencies" element={<CompetenciesPage />} />
          <Route path="reference/grades" element={<GradesPage />} />
          <Route path="reference/weight-presets" element={<WeightPresetsPage />} />
          <Route path="reference/planning-params" element={<PlanningParamsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
