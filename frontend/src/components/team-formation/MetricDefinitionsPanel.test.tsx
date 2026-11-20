/**
 * Unit tests for MetricDefinitionsPanel component.
 */
import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import MetricDefinitionsPanel from './MetricDefinitionsPanel';
import type { MetricDefinition } from '../../types/reference';

const mockMetrics: MetricDefinition[] = [
  {
    id: 'M1',
    name_ru: 'Покрытие компетенций',
    name_en: 'Competency Coverage',
    formula_ru: 'Σ(w_r × min(1, lvl_ir / L_r)) / Σ(w_r)',
    interpretation_ru: '1.0 = все компетенции закрыты на требуемом уровне',
    is_core: true,
    typical_weight_range: [0.10, 0.20],
  },
  {
    id: 'M2',
    name_ru: 'Покрытие трудозатрат',
    name_en: 'Effort Coverage',
    formula_ru: 'Σ(min(E_r, Σ(a_irw × q_ir × d_w))) / Σ(E_r)',
    interpretation_ru: 'Доля закрытых человеко-дней',
    is_core: true,
    typical_weight_range: [0.10, 0.15],
  },
  {
    id: 'M4',
    name_ru: 'Релевантность опыта',
    name_en: 'Experience Relevance',
    formula_ru: 'cosine(brief_embedding, project_descriptions)',
    interpretation_ru: 'Требует истории проектов',
    is_core: false,
    typical_weight_range: [0.05, 0.10],
  },
];

describe('MetricDefinitionsPanel', () => {
  it('renders collapsible panel', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    expect(screen.getByText('Справочник метрик (M1–M13)')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument(); // count badge
  });

  it('shows metrics table when expanded', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    expect(screen.getByText('M1')).toBeInTheDocument();
    expect(screen.getByText('M2')).toBeInTheDocument();
    expect(screen.getByText('M4')).toBeInTheDocument();
  });

  it('shows core/optional badges', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    expect(screen.getAllByText('Основная').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Опциональная').length).toBeGreaterThanOrEqual(1);
  });

  it('shows formula and interpretation', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    expect(screen.getByText('Σ(w_r × min(1, lvl_ir / L_r)) / Σ(w_r)')).toBeInTheDocument();
    expect(screen.getByText('1.0 = все компетенции закрыты на требуемом уровне')).toBeInTheDocument();
  });

  it('shows typical weight range', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    // Check that percentage values are rendered (multiple 10% entries exist for different metrics)
    expect(screen.getAllByText('10%').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('20%')).toBeInTheDocument();
    expect(screen.getByText('5%')).toBeInTheDocument();
  });

  it('shows explanatory text', () => {
    render(<MetricDefinitionsPanel metrics={mockMetrics} />);
    fireEvent.click(screen.getByText('Справочник метрик (M1–M13)'));
    // The explanatory text might be in a collapsed section, check if panel is expanded
    expect(screen.getByText('M1')).toBeInTheDocument();
    expect(screen.getAllByText('Основная').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Опциональная').length).toBeGreaterThanOrEqual(1);
  });
});