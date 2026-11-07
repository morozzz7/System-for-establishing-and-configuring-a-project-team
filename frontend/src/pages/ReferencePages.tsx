/**
 * Reference data pages — read-only views for taxonomy, grades, presets, planning params.
 */
import {
  Table,
  Space,
  Typography,
  Descriptions,
  Card,
  Tag,
  Tabs,
  Collapse,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Role, Competency, GradeBand, WeightPreset } from '../types';
import {
  useRoles,
  useCompetencies,
  useGrades,
  useWeightPresets,
  usePlanningParams,
} from '../hooks';
import { Loading, Empty, ErrorDisplay } from '../components/shared';

const { Title, Text, Paragraph } = Typography;

// ──── Roles ────

export function RolesPage() {
  const roles = useRoles();
  if (roles.loading) return <Loading />;
  if (roles.error) return <ErrorDisplay error={roles.error} />;
  if (!roles.data) return <Empty />;

  const columns: ColumnsType<Role> = [
    { title: 'ID', dataIndex: 'role_id', key: 'role_id' },
    { title: 'Название (RU)', dataIndex: 'name_ru', key: 'name_ru' },
    { title: 'Название (EN)', dataIndex: 'name_en', key: 'name_en' },
    { title: 'Семейство', dataIndex: 'family', key: 'family', render: (f) => <Tag>{f}</Tag> },
    {
      title: 'Типичные грейды',
      dataIndex: 'typical_grades',
      key: 'typical_grades',
      render: (grades) => grades.map((g: string) => <Tag key={g}>{g}</Tag>),
    },
    {
      title: 'Основные компетенции',
      dataIndex: 'core_competencies',
      key: 'core_competencies',
      render: (comps) => comps.map((c: string) => <Tag key={c} color="blue">{c}</Tag>),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Роли</Title>
      <Table dataSource={roles.data} columns={columns} rowKey="role_id" pagination={false} />
    </Space>
  );
}

// ──── Competencies ────

export function CompetenciesPage() {
  const competencies = useCompetencies();
  if (competencies.loading) return <Loading />;
  if (competencies.error) return <ErrorDisplay error={competencies.error} />;
  if (!competencies.data) return <Empty />;

  const grouped = competencies.data.reduce<Record<string, Competency[]>>((acc, c) => {
    if (!acc[c.category]) acc[c.category] = [];
    acc[c.category].push(c);
    return acc;
  }, {});

  const columns: ColumnsType<Competency> = [
    { title: 'ID', dataIndex: 'competency_id', key: 'competency_id' },
    { title: 'Название (RU)', dataIndex: 'name_ru', key: 'name_ru' },
    { title: 'Название (EN)', dataIndex: 'name_en', key: 'name_en' },
    {
      title: 'Роли',
      dataIndex: 'roles',
      key: 'roles',
      render: (roles) => roles.map((r: string) => <Tag key={r}>{r}</Tag>),
    },
    {
      title: 'Синонимы',
      dataIndex: 'synonyms',
      key: 'synonyms',
      render: (syns) => syns.join(', '),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Компетенции</Title>
      <Collapse
        items={Object.entries(grouped).map(([cat, items]) => ({
          key: cat,
          label: `${cat} (${items.length})`,
          children: (
            <Table dataSource={items} columns={columns} rowKey="competency_id" pagination={false} size="small" />
          ),
        }))}
      />
    </Space>
  );
}

// ──── Grades ────

export function GradesPage() {
  const grades = useGrades();
  if (grades.loading) return <Loading />;
  if (grades.error) return <ErrorDisplay error={grades.error} />;
  if (!grades.data) return <Empty />;

  const columns: ColumnsType<GradeBand> = [
    { title: 'Грейд', dataIndex: 'grade', key: 'grade', render: (g) => <Tag>{g}</Tag> },
    { title: 'Название', dataIndex: 'label_ru', key: 'label_ru' },
    { title: 'Коэф. стоимости', dataIndex: 'cost_coefficient', key: 'cost_coefficient' },
    {
      title: 'Типичный диапазон уровней',
      dataIndex: 'typical_level_range',
      key: 'typical_level_range',
      render: (range) => `${range[0]}–${range[1]}`,
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Грейды</Title>
      <Table dataSource={grades.data} columns={columns} rowKey="grade" pagination={false} />
    </Space>
  );
}

// ──── Weight Presets ────

export function WeightPresetsPage() {
  const presets = useWeightPresets();
  if (presets.loading) return <Loading />;
  if (presets.error) return <ErrorDisplay error={presets.error} />;
  if (!presets.data) return <Empty />;

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Пресеты весов</Title>
      <Tabs
        items={presets.data.map((p) => ({
          key: p.preset,
          label: p.label_ru,
          children: <WeightPresetCard preset={p} />,
        }))}
      />
    </Space>
  );
}

function WeightPresetCard({ preset }: { preset: WeightPreset }) {
  const metrics = [
    { key: 'M1', label: 'Покрытие компетенций' },
    { key: 'M2', label: 'Покрытие усилий' },
    { key: 'M3', label: 'Соответствие уровня' },
    { key: 'M5', label: 'Расписание' },
    { key: 'M6', label: 'Стоимость' },
    { key: 'M7', label: 'Здоровье загрузки' },
    { key: 'M8', label: 'Влияние на портфель' },
    { key: 'M9', label: 'Bus factor' },
    { key: 'M10', label: 'Фокус' },
    { key: 'M11', label: 'Утилизация скамейки' },
    { key: 'M12', label: 'Устойчивость' },
  ];

  return (
    <Card>
      <Descriptions column={2} bordered size="small">
        {metrics.map((m) => (
          <Descriptions.Item key={m.key} label={`${m.key}: ${m.label}`}>
            {((preset.weights as unknown as Record<string, number>)[m.key] * 100).toFixed(0)}%
          </Descriptions.Item>
        ))}
        <Descriptions.Item label="Сумма">
          <Text strong>{preset.sum_check}</Text>
        </Descriptions.Item>
      </Descriptions>
    </Card>
  );
}

// ──── Planning Params ────

export function PlanningParamsPage() {
  const params = usePlanningParams();
  if (params.loading) return <Loading />;
  if (params.error) return <ErrorDisplay error={params.error} />;
  if (!params.data) return <Empty />;

  const p = params.data;

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Параметры планирования</Title>
      <Card title="Временная модель">
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="Начало недели">{p.time_model.week_start}</Descriptions.Item>
          <Descriptions.Item label="Рабочих дней/неделя">
            {p.time_model.working_days_per_week}
          </Descriptions.Item>
          <Descriptions.Item label="Часов/день">{p.time_model.hours_per_day}</Descriptions.Item>
          <Descriptions.Item label="Норма часов/неделя">
            {p.time_model.weekly_hours_norm}
          </Descriptions.Item>
          <Descriptions.Item label="Коэф. резерва">
            {p.time_model.reserve_ratio}
          </Descriptions.Item>
          <Descriptions.Item label="Календарь">{p.time_model.calendar}</Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="Расписание">
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="Monte Carlo прогонов">
            {p.schedule.monte_carlo_runs}
          </Descriptions.Item>
          <Descriptions.Item label="Seed">{p.schedule.monte_carlo_seed}</Descriptions.Item>
          <Descriptions.Item label="Portfolio tau (раб. дней)">
            {p.schedule.portfolio_tau_working_days}
          </Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="Солвер">
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="Лимит времени (мс)">
            {p.solver.default_time_limit_ms}
          </Descriptions.Item>
          <Descriptions.Item label="Лимит пересчёта (мс)">
            {p.solver.recalc_time_limit_ms}
          </Descriptions.Item>
          <Descriptions.Item label="k min">{p.solver.k_min}</Descriptions.Item>
          <Descriptions.Item label="k max">{p.solver.k_max}</Descriptions.Item>
          <Descriptions.Item label="Min Jaccard diversity">
            {p.solver.min_jaccard_diversity}
          </Descriptions.Item>
        </Descriptions>
      </Card>
      <Card title="Лимиты">
        <Descriptions column={2} bordered size="small">
          <Descriptions.Item label="Макс. сотрудников">{p.limits.max_employees}</Descriptions.Item>
          <Descriptions.Item label="Макс. требований">{p.limits.max_requirements}</Descriptions.Item>
        </Descriptions>
      </Card>
      <Paragraph type="secondary">Версия метрик: {p.metrics_version}</Paragraph>
    </Space>
  );
}
