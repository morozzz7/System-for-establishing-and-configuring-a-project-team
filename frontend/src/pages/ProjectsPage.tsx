/**
 * Projects page — list with status filter.
 */
import { useState } from 'react';
import { Table, Select, Space, Typography, Tag, Button, Modal, Form, Input, InputNumber, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Project, ProjectStatus, ProjectCriticality } from '../types';
import { useProjects } from '../hooks';
import { Loading, Empty, ErrorDisplay, StatusTag } from '../components/shared';
import { createProject } from '../api/client';

const { Title } = Typography;

const STATUS_OPTIONS: ProjectStatus[] = ['planned', 'active', 'paused', 'done', 'cancelled'];
const CRITICALITY_OPTIONS: ProjectCriticality[] = ['critical', 'high', 'normal', 'low'];

export default function ProjectsPage() {
  const projects = useProjects();
  const [statusFilter, setStatusFilter] = useState<string | undefined>();
  const [createOpen, setCreateOpen] = useState(false);
  const [form] = Form.useForm();

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
    },
    {
      title: 'Название',
      dataIndex: 'name',
      key: 'name',
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

  const handleCreate = async (values: Record<string, unknown>) => {
    try {
      await createProject({
        project_id: `prj_${Math.random().toString(36).slice(2, 8)}`,
        code: values.code as string,
        name: values.name as string,
        status: values.status as ProjectStatus,
        priority: values.priority as number,
        criticality: values.criticality as ProjectCriticality,
        start_week: values.start_week as string,
        end_week_planned: values.end_week_planned as string,
        replaces_other_projects: false,
      });
      message.success('Проект создан (мок)');
      setCreateOpen(false);
      form.resetFields();
      projects.refetch();
    } catch {
      message.error('Ошибка создания проекта');
    }
  };

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Title level={2} style={{ margin: 0 }}>Проекты</Title>
        <Button type="primary" onClick={() => setCreateOpen(true)}>
          Создать проект
        </Button>
      </Space>
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
      <Table dataSource={filtered} columns={columns} rowKey="project_id" pagination={{ pageSize: 10 }} />

      <Modal
        title="Создать проект"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => form.submit()}
      >
        <Form form={form} layout="vertical" onFinish={handleCreate}>
          <Form.Item name="code" label="Код" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="name" label="Название" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="status" label="Статус" initialValue="planned" rules={[{ required: true }]}>
            <Select options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))} />
          </Form.Item>
          <Form.Item name="priority" label="Приоритет (1-5)" initialValue={3} rules={[{ required: true }]}>
            <InputNumber min={1} max={5} />
          </Form.Item>
          <Form.Item name="criticality" label="Критичность" initialValue="normal" rules={[{ required: true }]}>
            <Select options={CRITICALITY_OPTIONS.map((c) => ({ value: c, label: c }))} />
          </Form.Item>
          <Form.Item name="start_week" label="Начало (неделя)" initialValue="2026-W10" rules={[{ required: true }]}>
            <Input placeholder="2026-W10" />
          </Form.Item>
          <Form.Item name="end_week_planned" label="Окончание (неделя)" initialValue="2026-W24" rules={[{ required: true }]}>
            <Input placeholder="2026-W24" />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
