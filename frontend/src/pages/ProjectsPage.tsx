/**
 * Projects page — list with status filter. Projects are created when a manager approves a team
 * variant on the run screen, so there is no manual create action here.
 */
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Table, Select, Space, Typography, Tag } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Project, ProjectStatus } from '../types';
import { useProjects } from '../hooks';
import { Loading, Empty, ErrorDisplay, StatusTag } from '../components/shared';

const { Title } = Typography;

const STATUS_OPTIONS: ProjectStatus[] = ['planned', 'active', 'paused', 'done', 'cancelled'];

export default function ProjectsPage() {
  const projects = useProjects();
  const [statusFilter, setStatusFilter] = useState<string | undefined>();

  if (projects.loading) return <Loading />;
  if (projects.error) return <ErrorDisplay error={projects.error} />;
  if (!projects.data) return <Empty />;

  const filtered = statusFilter
    ? projects.data.filter((p) => p.status === statusFilter)
    : projects.data;

  const columns: ColumnsType<Project> = [
    {
      title: 'Код',
      dataIndex: 'code',
      key: 'code',
      sorter: (a, b) => a.code.localeCompare(b.code),
      render: (code: string, record) => <Link to={`/projects/${record.project_id}`}>{code}</Link>,
    },
    {
      title: 'Название',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record) => <Link to={`/projects/${record.project_id}`}>{name}</Link>,
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      render: (status) => <StatusTag status={status} />,
      filters: STATUS_OPTIONS.map((s) => ({ text: s, value: s })),
    },
    {
      title: 'Приоритет',
      dataIndex: 'priority',
      key: 'priority',
      render: (p) => <Tag>{p}</Tag>,
      sorter: (a, b) => a.priority - b.priority,
    },
    {
      title: 'Критичность',
      dataIndex: 'criticality',
      key: 'criticality',
      render: (c) => <Tag color={c === 'critical' ? 'red' : c === 'high' ? 'orange' : 'default'}>{c}</Tag>,
    },
    {
      title: 'Начало',
      dataIndex: 'start_week',
      key: 'start_week',
    },
    {
      title: 'План. окончание',
      dataIndex: 'end_week_planned',
      key: 'end_week_planned',
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2} style={{ margin: 0 }}>Проекты</Title>
      <Space>
        <Select
          placeholder="Статус"
          value={statusFilter}
          onChange={setStatusFilter}
          style={{ width: 200 }}
          allowClear
          options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))}
        />
      </Space>
      <Table
        dataSource={filtered}
        columns={columns}
        rowKey="project_id"
        pagination={{ pageSize: 10 }}
        locale={{ emptyText: 'Проекты появятся здесь после утверждения состава команды' }}
      />
    </Space>
  );
}
