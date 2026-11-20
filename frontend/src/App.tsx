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
import BriefsPage from './pages/BriefsPage';
import BriefReviewPage from './pages/BriefReviewPage';
import RunPage from './pages/RunPage';
import RunConfigPage from './pages/RunConfigPage';
import NotFoundPage from './pages/NotFoundPage';

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<AppLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="briefs" element={<BriefsPage />} />
          <Route path="briefs/:briefId/review" element={<BriefReviewPage />} />
          <Route path="runs/new/:briefId" element={<RunPage />} />
          <Route path="runs/config" element={<RunConfigPage />} />
          <Route path="runs/config/:briefId" element={<RunConfigPage />} />
          <Route path="employees" element={<EmployeesPage />} />
          <Route path="projects" element={<ProjectsPage />} />
          <Route path="assignments" element={<AssignmentsPage />} />
          <Route path="absences" element={<AbsencesPage />} />
          <Route path="reference/roles" element={<RolesPage />} />
          <Route path="reference/competencies" element={<CompetenciesPage />} />
          <Route path="reference/grades" element={<GradesPage />} />
          <Route path="reference/weight-presets" element={<WeightPresetsPage />} />
          <Route path="reference/planning-params" element={<PlanningParamsPage />} />
          {/* Catch-all: any unmatched path renders the in-app 404 screen instead of a blank view. */}
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
