/**
 * Project detail — the approved team as a plan: weekly assignments, their staffing summary and
 * absences of the people assigned to this project. All values come from the API.
 */
import { Link, useParams } from 'react-router-dom';
import { Card, Col, Descriptions, Row, Space, Statistic, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Absence, Assignment } from '../types';
import { useAssignments, useAbsences, useProjects, useRoles } from '../hooks';
import { Empty, ErrorDisplay, Loading, StatusTag } from '../components/shared';

const { Title, Text } = Typography;

const ABSENCE_TYPE_LABELS: Record<string, string> = {
  vacation: 'Отпуск',
  sick_leave: 'Больничный',
  training: 'Обучение',
  business_trip: 'Командировка',
  parental_leave: 'Декрет',
  other: 'Другое',
};

const SOURCE_LABELS: Record<string, string> = {
  plan: 'план',
  actual: 'факт',
  forecast: 'прогноз',
};

const CRITICALITY_LABELS: Record<string, string> = {
  critical: 'критичная',
  high: 'высокая',
  normal: 'обычная',
  low: 'низкая',
};

export default function ProjectDetailPage() {
  const { projectId = '' } = useParams();
  const projects = useProjects();
  const assignments = useAssignments(projectId);
  const absences = useAbsences();
  const roles = useRoles();

  if (projects.loading || assignments.loading || absences.loading) return <Loading />;
  if (projects.error || assignments.error || absences.error)
    return <ErrorDisplay error={projects.error ?? assignments.error ?? absences.error ?? ''} />;

  const project = projects.data?.find((item) => item.project_id === projectId);
  if (!project) return <Empty message="Проект не найден" />;

  const roleNames = new Map((roles.data ?? []).map((role) => [role.role_id, role.name_ru]));

  const planned = assignments.data ?? [];
  const assignedIds = new Set(planned.map((item) => item.employee_id));
  const weeks = new Set(planned.map((item) => item.week));
  const totalHours = planned.reduce((sum, item) => sum + item.hours, 0);
  const hoursByWeek = new Map<string, number>();
  for (const item of planned) {
    hoursByWeek.set(item.week, (hoursByWeek.get(item.week) ?? 0) + item.hours);
  }
  const peakWeek = [...hoursByWeek].sort((a, b) => b[1] - a[1])[0];
  const teamAbsences = (absences.data ?? []).filter((item) => assignedIds.has(item.employee_id));

  const assignmentColumns: ColumnsType<Assignment> = [
    {
      title: 'Сотрудник',
      dataIndex: 'employee_id',
      key: 'employee_id',
      sorter: (a, b) => a.employee_id.localeCompare(b.employee_id),
      render: (id: string) => <Text code>{id}</Text>,
    },
    {
      title: 'Роль',
      dataIndex: 'role_id',
      key: 'role_id',
      render: (id: string) => roleNames.get(id) ?? id,
    },
    {
      title: 'Неделя',
      dataIndex: 'week',
      key: 'week',
      sorter: (a, b) => a.week.localeCompare(b.week),
    },
    {
      title: 'Часы',
      dataIndex: 'hours',
      key: 'hours',
      sorter: (a, b) => a.hours - b.hours,
      render: (hours: number) => `${hours} ч`,
    },
    {
      title: 'Источник',
      dataIndex: 'source',
      key: 'source',
      render: (source: string) => <StatusTag status={SOURCE_LABELS[source] ?? source} />,
    },
  ];

  const absenceColumns: ColumnsType<Absence> = [
    {
      title: 'Сотрудник',
      dataIndex: 'employee_id',
      key: 'employee_id',
      sorter: (a, b) => a.employee_id.localeCompare(b.employee_id),
      render: (id: string) => <Text code>{id}</Text>,
    },
    {
      title: 'Тип',
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => ABSENCE_TYPE_LABELS[type] ?? type,
    },
    { title: 'Начало', dataIndex: 'start_date', key: 'start_date' },
    { title: 'Окончание', dataIndex: 'end_date', key: 'end_date' },
    {
      title: 'Часов/день',
      dataIndex: 'hours_per_day',
      key: 'hours_per_day',
      render: (hours: number) => `${hours} ч`,
    },
    {
      title: 'Одобрено',
      dataIndex: 'approved',
      key: 'approved',
      render: (approved: boolean) => <StatusTag status={approved ? 'done' : 'paused'} />,
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }} data-testid="project-detail">
      <Space direction="vertical" size={0}>
        <Link to="/projects">← Все проекты</Link>
        <Title level={2} style={{ margin: 0 }}>
          {project.name}
        </Title>
      </Space>

      <Card size="small">
        <Descriptions column={{ xs: 1, sm: 2, lg: 3 }} size="small">
          <Descriptions.Item label="Код">{project.code}</Descriptions.Item>
          <Descriptions.Item label="Статус">
            <StatusTag status={project.status} />
          </Descriptions.Item>
          <Descriptions.Item label="Приоритет">{project.priority}</Descriptions.Item>
          <Descriptions.Item label="Критичность">
            {CRITICALITY_LABELS[project.criticality] ?? project.criticality}
          </Descriptions.Item>
          <Descriptions.Item label="Начало">{project.start_week}</Descriptions.Item>
          <Descriptions.Item label="План. окончание">{project.end_week_planned}</Descriptions.Item>
          {project.end_week_forecast && (
            <Descriptions.Item label="Прогноз">{project.end_week_forecast}</Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      <Row gutter={[12, 12]}>
        <Col xs={12} md={6}>
          <Card size="small">
            <Statistic title="Сотрудников" value={assignedIds.size} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card size="small">
            <Statistic title="Плановых часов" value={totalHours} suffix="ч" />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card size="small">
            <Statistic title="Недель" value={weeks.size} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card size="small">
            <Statistic title="Пиковая неделя" value={peakWeek?.[0] ?? '—'} suffix={peakWeek ? `${peakWeek[1]} ч` : undefined} />
          </Card>
        </Col>
      </Row>

      <Card size="small" title="Команда и назначения">
        <Table
          dataSource={planned}
          columns={assignmentColumns}
          rowKey="assignment_id"
          size="small"
          pagination={{ pageSize: 15 }}
          locale={{ emptyText: 'Назначения появятся после утверждения варианта команды' }}
          data-testid="project-assignments"
        />
      </Card>

      <Card size="small" title="Отсутствия команды">
        <Table
          dataSource={teamAbsences}
          columns={absenceColumns}
          rowKey="absence_id"
          size="small"
          pagination={{ pageSize: 10 }}
          locale={{ emptyText: 'У назначенной команды нет записей об отсутствии' }}
          data-testid="project-absences"
        />
      </Card>
    </Space>
  );
}
