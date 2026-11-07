/**
 * Dashboard page — summary stats from mock data.
 */
import { Card, Col, Row, Statistic, Typography, Space } from 'antd';
import {
  TeamOutlined,
  ProjectOutlined,
  CalendarOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { useEmployees, useProjects, useAbsences } from '../hooks';
import { Loading, Empty, ErrorDisplay } from '../components/shared';

const { Title } = Typography;

export default function DashboardPage() {
  const employees = useEmployees();
  const projects = useProjects();
  const absences = useAbsences();

  if (employees.loading || projects.loading || absences.loading) return <Loading />;
  if (employees.error || projects.error || absences.error)
    return <ErrorDisplay error={employees.error ?? projects.error ?? absences.error ?? ''} />;
  if (!employees.data || !projects.data || !absences.data) return <Empty />;

  const activeEmployees = employees.data.filter((e) => e.active).length;
  const benchCount = employees.data.filter((e) => e.bench_flag).length;
  const activeProjects = projects.data.filter((p) => p.status === 'active').length;
  const plannedProjects = projects.data.filter((p) => p.status === 'planned').length;
  const activeAbsences = absences.data.filter((a) => a.approved).length;

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Дашборд</Title>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Сотрудники (активные)"
              value={activeEmployees}
              prefix={<TeamOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="На скамейке"
              value={benchCount}
              prefix={<UserOutlined />}
              valueStyle={{ color: benchCount > 0 ? '#3f8600' : undefined }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Проекты (активные)"
              value={activeProjects}
              prefix={<ProjectOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Запланировано"
              value={plannedProjects}
              prefix={<CalendarOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Одобренные отсутствия"
              value={activeAbsences}
              prefix={<CalendarOutlined />}
            />
          </Card>
        </Col>
      </Row>
    </Space>
  );
}
