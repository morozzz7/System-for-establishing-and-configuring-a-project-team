/**
 * Absences page — table/calendar of absences.
 */
import { Table, Button, Space, Typography, Tag, Popconfirm, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { Absence } from '../types';
import { useAbsences } from '../hooks';
import { Loading, Empty, ErrorDisplay, StatusTag } from '../components/shared';
import { deleteAbsence } from '../api/client';

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

  if (absences.loading) return <Loading />;
  if (absences.error) return <ErrorDisplay error={absences.error} />;
  if (!absences.data) return <Empty />;

  const handleDelete = async (absenceId: string) => {
    try {
      await deleteAbsence(absenceId);
      message.success('Отсутствие удалено (мок)');
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
      <Title level={2}>Отсутствия</Title>
      <Table
        dataSource={absences.data}
        columns={columns}
        rowKey="absence_id"
        pagination={{ pageSize: 10 }}
      />
    </Space>
  );
}
