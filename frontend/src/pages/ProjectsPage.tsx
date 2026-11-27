import { formatWeekDate } from '../utils/date';
/**
 * Projects page — list with status filter. Projects are created when a manager approves a team
 * variant on the run screen, so there is no manual create action here.
 */
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Popconfirm, Table, Select, Space, Typography, Tag, message } from 'antd';
import { DeleteOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { Project, ProjectStatus } from '../types';
import { useProjects } from '../hooks';
import { deleteProject } from '../api/client';
import { Loading, Empty, ErrorDisplay, StatusTag } from '../components/shared';

const { Title } = Typography;

const STATUS_OPTIONS: ProjectStatus[] = ['planned', 'active', 'paused', 'done', 'cancelled'];

export default function ProjectsPage() {
  const projects = useProjects();
  const [statusFilter, setStatusFilter] = useState<string | undefined>();
  const [deletingId, setDeletingId] = useState<string | undefined>();

  const handleDelete = async (projectId: string) => {
    setDeletingId(projectId);
    try {
      await deleteProject(projectId);
      message.success('Проект удалён из портфеля');
      projects.refetch();
    } catch (err) {
      message.error(`Ошибка удаления проекта: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setDeletingId(undefined);
    }
  };

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
      render: (value: string) => formatWeekDate(value),
    },
    {
      title: 'План. окончание',
      dataIndex: 'end_week_planned',
      key: 'end_week_planned',
      render: (value: string) => formatWeekDate(value, true),
    },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      render: (_, record) => (
        <Popconfirm
          title="Удалить проект и его назначения?"
          okText="Удалить"
          cancelText="Отмена"
          onConfirm={() => handleDelete(record.project_id)}
        >
          <Button
            danger
            size="small"
            icon={<DeleteOutlined />}
            loading={deletingId === record.project_id}
            data-testid={`delete-project-${record.project_id}`}
          >
            Удалить
          </Button>
        </Popconfirm>
      ),
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
