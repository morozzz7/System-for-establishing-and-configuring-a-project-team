/**
 * Assignments page — table of current assignments.
 */
import { Table, Space, Typography, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Assignment } from '../types';
import { useAssignments } from '../hooks';
import { Loading, Empty, ErrorDisplay } from '../components/shared';
import { mockRoles } from '../mocks/reference';

const { Title } = Typography;

export default function AssignmentsPage() {
  const assignments = useAssignments();

  if (assignments.loading) return <Loading />;
  if (assignments.error) return <ErrorDisplay error={assignments.error} />;
  if (!assignments.data) return <Empty />;

  const columns: ColumnsType<Assignment> = [
    {
      title: 'ID',
      dataIndex: 'assignment_id',
      key: 'assignment_id',
    },
    {
      title: 'Сотрудник',
      dataIndex: 'employee_id',
      key: 'employee_id',
      render: (id) => <Tag>{id}</Tag>,
      sorter: (a, b) => a.employee_id.localeCompare(b.employee_id),
    },
    {
      title: 'Проект',
      dataIndex: 'project_id',
      key: 'project_id',
      render: (id) => <Tag color="blue">{id}</Tag>,
    },
    {
      title: 'Роль',
      dataIndex: 'role_id',
      key: 'role_id',
      render: (id) => mockRoles.find((r) => r.role_id === id)?.name_ru ?? id,
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
      render: (h) => `${h} ч`,
      sorter: (a, b) => a.hours - b.hours,
    },
    {
      title: 'Источник',
      dataIndex: 'source',
      key: 'source',
      render: (s) => <Tag>{s}</Tag>,
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Назначения</Title>
      <Table
        dataSource={assignments.data}
        columns={columns}
        rowKey="assignment_id"
        pagination={{ pageSize: 15 }}
      />
    </Space>
  );
}
