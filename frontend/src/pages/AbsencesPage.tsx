/**
 * Absences page — table/calendar of absences.
 */
import { useState } from 'react';
import { Table, Button, Space, Typography, Tag, Popconfirm, Modal, Form, Input, InputNumber, Select, Switch, message } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { Absence } from '../types';
import { useAbsences, useEmployees } from '../hooks';
import { Loading, Empty, ErrorDisplay, StatusTag } from '../components/shared';
import { createAbsence, deleteAbsence } from '../api/client';

const { Title } = Typography;

const ABSENCE_TYPE_LABELS: Record<string, string> = {
  vacation: 'Отпуск',
  sick_leave: 'Больничный',
  training: 'Обучение',
  business_trip: 'Командировка',
  parental_leave: 'Декрет',
  other: 'Другое',
};

export default function AbsencesPage() {
  const absences = useAbsences();
  const employees = useEmployees();
  const [form] = Form.useForm();
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    const values = (await form.validateFields()) as {
      employee_id: string;
      type: string;
      start_date: string;
      end_date: string;
      hours_per_day: number;
      approved: boolean;
    };
    setCreating(true);
    try {
      await createAbsence({
        employee_id: values.employee_id,
        type: values.type as Absence['type'],
        start_date: values.start_date,
        end_date: values.end_date,
        hours_per_day: Number(values.hours_per_day),
        approved: Boolean(values.approved),
        source: 'manual',
      });
      message.success('Отсутствие добавлено');
      setCreateOpen(false);
      form.resetFields();
      absences.refetch();
    } catch (err) {
      message.error(`Ошибка добавления: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setCreating(false);
    }
  };

  if (absences.loading) return <Loading />;
  if (absences.error) return <ErrorDisplay error={absences.error} />;
  if (!absences.data) return <Empty />;

  const handleDelete = async (absenceId: string) => {
    try {
      await deleteAbsence(absenceId);
      message.success('Отсутствие удалено');
      absences.refetch();
    } catch {
      message.error('Ошибка удаления');
    }
  };

  const columns: ColumnsType<Absence> = [
    {
      title: 'ID',
      dataIndex: 'absence_id',
      key: 'absence_id',
    },
    {
      title: 'Сотрудник',
      dataIndex: 'employee_id',
      key: 'employee_id',
      render: (id) => <Tag>{id}</Tag>,
      sorter: (a, b) => a.employee_id.localeCompare(b.employee_id),
    },
    {
      title: 'Тип',
      dataIndex: 'type',
      key: 'type',
      render: (t) => <StatusTag status={t} />,
      filters: Object.entries(ABSENCE_TYPE_LABELS).map(([k, v]) => ({ text: v, value: k })),
      onFilter: (value, record) => record.type === value,
    },
    {
      title: 'Начало',
      dataIndex: 'start_date',
      key: 'start_date',
      sorter: (a, b) => a.start_date.localeCompare(b.start_date),
    },
    {
      title: 'Окончание',
      dataIndex: 'end_date',
      key: 'end_date',
    },
    {
      title: 'Часов/день',
      dataIndex: 'hours_per_day',
      key: 'hours_per_day',
      render: (h) => `${h} ч`,
    },
    {
      title: 'Одобрено',
      dataIndex: 'approved',
      key: 'approved',
      render: (v) => (v ? <Tag color="green">Да</Tag> : <Tag color="red">Нет</Tag>),
    },
    {
      title: 'Источник',
      dataIndex: 'source',
      key: 'source',
    },
    {
      title: '',
      key: 'actions',
      render: (_, record) => (
        <Popconfirm
          title="Удалить отсутствие?"
          onConfirm={() => handleDelete(record.absence_id)}
          okText="Да"
          cancelText="Нет"
        >
          <Button danger size="small">
            Удалить
          </Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Title level={2} style={{ margin: 0 }}>Отсутствия</Title>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => setCreateOpen(true)}
          data-testid="create-absence"
        >
          Добавить отсутствие
        </Button>
      </Space>
      <Table
        dataSource={absences.data}
        columns={columns}
        rowKey="absence_id"
        pagination={{ pageSize: 10 }}
      />

      <Modal
        title="Новое отсутствие"
        open={createOpen}
        onCancel={() => {
          setCreateOpen(false);
          form.resetFields();
        }}
        onOk={handleCreate}
        okText="Добавить"
        cancelText="Отмена"
        confirmLoading={creating}
      >
        <Form form={form} layout="vertical" initialValues={{ type: 'vacation', hours_per_day: 8, approved: true }}>
          <Form.Item
            name="employee_id"
            label="Сотрудник"
            rules={[{ required: true, message: 'Выберите сотрудника' }]}
          >
            <Select
              showSearch
              placeholder="emp_xxxx"
              loading={employees.loading}
              options={(employees.data ?? []).map((emp) => ({
                value: emp.employee_id,
                label: `${emp.employee_id} · ${emp.grade}`,
              }))}
              filterOption={(input, option) => String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())}
            />
          </Form.Item>
          <Form.Item name="type" label="Тип" rules={[{ required: true }]}>
            <Select
              options={Object.entries(ABSENCE_TYPE_LABELS).map(([value, label]) => ({ value, label }))}
            />
          </Form.Item>
          <Form.Item name="start_date" label="Начало" rules={[{ required: true }]}>
            <Input type="date" />
          </Form.Item>
          <Form.Item name="end_date" label="Окончание" rules={[{ required: true }]}>
            <Input type="date" />
          </Form.Item>
          <Form.Item name="hours_per_day" label="Часов в день" rules={[{ required: true }]}>
            <InputNumber min={0} max={12} step={0.5} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="approved" label="Одобрено" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
