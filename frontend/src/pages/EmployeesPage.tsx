/**
 * Employees page — list with filters + detail page.
 */
import { useState } from 'react';
import {
  Table,
  Input,
  Select,
  Space,
  Typography,
  Button,
  Drawer,
  Descriptions,
  Card,
  Tag,
} from 'antd';
import { EyeOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { Employee } from '../types';
import { useEmployees, useEmployee, useEmployeeCompetencies, useEmployeeCapacity } from '../hooks';
import { Loading, Empty, ErrorDisplay, GradeTag, LevelBar } from '../components/shared';
import { mockRoles } from '../mocks/reference';

const { Title, Text } = Typography;

export default function EmployeesPage() {
  const employees = useEmployees();
  const [search, setSearch] = useState('');
  const [gradeFilter, setGradeFilter] = useState<string | undefined>();
  const [roleFilter, setRoleFilter] = useState<string | undefined>();
  const [benchFilter, setBenchFilter] = useState<boolean | undefined>();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (employees.loading) return <Loading />;
  if (employees.error) return <ErrorDisplay error={employees.error} />;
  if (!employees.data) return <Empty />;

  let filtered = employees.data;
  if (search) {
    const q = search.toLowerCase();
    filtered = filtered.filter((e) => e.employee_id.toLowerCase().includes(q));
  }
  if (gradeFilter) filtered = filtered.filter((e) => e.grade === gradeFilter);
  if (roleFilter) filtered = filtered.filter((e) => e.role_profile_id === roleFilter);
  if (benchFilter !== undefined) filtered = filtered.filter((e) => e.bench_flag === benchFilter);

  const columns: ColumnsType<Employee> = [
    {
      title: 'ID',
      dataIndex: 'employee_id',
      key: 'employee_id',
      sorter: (a, b) => a.employee_id.localeCompare(b.employee_id),
    },
    {
      title: 'Грейд',
      dataIndex: 'grade',
      key: 'grade',
      render: (grade) => <GradeTag grade={grade} />,
      sorter: (a, b) => a.grade.localeCompare(b.grade),
    },
    {
      title: 'Роль',
      dataIndex: 'role_profile_id',
      key: 'role_profile_id',
      render: (id) => mockRoles.find((r) => r.role_id === id)?.name_ru ?? id,
    },
    {
      title: 'Тип занятости',
      dataIndex: 'employment_type',
      key: 'employment_type',
    },
    {
      title: 'Скамейка',
      dataIndex: 'bench_flag',
      key: 'bench_flag',
      render: (flag) => (flag ? <Tag color="green">Да</Tag> : <Tag>Нет</Tag>),
    },
    {
      title: 'Активен',
      dataIndex: 'active',
      key: 'active',
      render: (flag) => (flag ? <Tag color="blue">Да</Tag> : <Tag color="red">Нет</Tag>),
    },
    {
      title: '',
      key: 'actions',
      render: (_, record) => (
        <Button icon={<EyeOutlined />} onClick={() => setSelectedId(record.employee_id)}>
          Детали
        </Button>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Сотрудники</Title>
      <Space wrap>
        <Input.Search
          placeholder="Поиск по ID"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ width: 200 }}
          allowClear
        />
        <Select
          placeholder="Грейд"
          value={gradeFilter}
          onChange={setGradeFilter}
          style={{ width: 120 }}
          allowClear
          options={[
            { value: 'J', label: 'J' },
            { value: 'M', label: 'M' },
            { value: 'S', label: 'S' },
            { value: 'L', label: 'L' },
            { value: 'P', label: 'P' },
          ]}
        />
        <Select
          placeholder="Роль"
          value={roleFilter}
          onChange={setRoleFilter}
          style={{ width: 200 }}
          allowClear
          options={mockRoles.map((r) => ({ value: r.role_id, label: r.name_ru }))}
        />
        <Select
          placeholder="Скамейка"
          value={benchFilter}
          onChange={setBenchFilter}
          style={{ width: 150 }}
          allowClear
          options={[
            { value: true, label: 'На скамейке' },
            { value: false, label: 'Не на скамейке' },
          ]}
        />
      </Space>
      <Table
        dataSource={filtered}
        columns={columns}
        rowKey="employee_id"
        pagination={{ pageSize: 10 }}
      />
      {selectedId && <EmployeeDetailDrawer employeeId={selectedId} onClose={() => setSelectedId(null)} />}
    </Space>
  );
}

function EmployeeDetailDrawer({
  employeeId,
  onClose,
}: {
  employeeId: string;
  onClose: () => void;
}) {
  const employee = useEmployee(employeeId);
  const competencies = useEmployeeCompetencies(employeeId);
  const capacity = useEmployeeCapacity(employeeId, '2026-W05', '2026-W10');

  if (employee.loading || competencies.loading) return null;
  if (employee.error || !employee.data) return null;

  const emp = employee.data;
  const role = mockRoles.find((r) => r.role_id === emp.role_profile_id);

  return (
    <Drawer title={`Сотрудник: ${emp.employee_id}`} open onClose={onClose} width={600}>
      <Descriptions column={1} bordered size="small">
        <Descriptions.Item label="ID">{emp.employee_id}</Descriptions.Item>
        <Descriptions.Item label="Грейд">
          <GradeTag grade={emp.grade} />
        </Descriptions.Item>
        <Descriptions.Item label="Роль">{role?.name_ru ?? emp.role_profile_id}</Descriptions.Item>
        <Descriptions.Item label="Подразделение">{emp.unit_id}</Descriptions.Item>
        <Descriptions.Item label="Тип занятости">{emp.employment_type}</Descriptions.Item>
        <Descriptions.Item label="На скамейке">
          {emp.bench_flag ? 'Да' : 'Нет'}
        </Descriptions.Item>
        <Descriptions.Item label="Активен">{emp.active ? 'Да' : 'Нет'}</Descriptions.Item>
        <Descriptions.Item label="Коэф. стоимости">{emp.cost_coefficient}</Descriptions.Item>
        <Descriptions.Item label="Норма часов/нед">
          {emp.capacity_profile.weekly_hours_norm}
        </Descriptions.Item>
        <Descriptions.Item label="Резерв">
          {Math.round(emp.capacity_profile.reserve_ratio * 100)}%
        </Descriptions.Item>
        <Descriptions.Item label="Макс. загрузка">
          {Math.round(emp.capacity_profile.max_utilization_ratio * 100)}%
        </Descriptions.Item>
      </Descriptions>

      <Title level={5} style={{ marginTop: 24 }}>
        Компетенции
      </Title>
      {competencies.loading ? (
        <Loading />
      ) : competencies.data && competencies.data.length > 0 ? (
        <Space direction="vertical" style={{ width: '100%' }}>
          {competencies.data.map((c) => (
            <Card key={c.competency_id} size="small">
              <Text strong>{c.competency_id}</Text>
              <div>
                <LevelBar level={c.level} confidence={c.confidence} />
              </div>
              {c.last_used && (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  Последнее использование: {c.last_used}
                </Text>
              )}
            </Card>
          ))}
        </Space>
      ) : (
        <Empty message="Компетенции не указаны" />
      )}

      <Title level={5} style={{ marginTop: 24 }}>
        Ёмкость по неделям
      </Title>
      {capacity.loading ? (
        <Loading />
      ) : capacity.data && capacity.data.length > 0 ? (
        <Table
          dataSource={capacity.data}
          rowKey="week"
          size="small"
          pagination={false}
          columns={[
            { title: 'Неделя', dataIndex: 'week', key: 'week' },
            { title: 'Рабочих дней', dataIndex: 'working_days', key: 'working_days' },
            { title: 'Норма', dataIndex: 'norm_hours', key: 'norm_hours' },
            { title: 'Назначено', dataIndex: 'assigned_hours', key: 'assigned_hours' },
            { title: 'Свободно', dataIndex: 'free_hours', key: 'free_hours' },
            {
              title: 'Загрузка',
              dataIndex: 'load_ratio',
              key: 'load_ratio',
              render: (v) => `${Math.round(v * 100)}%`,
            },
            {
              title: 'Недоступен',
              dataIndex: 'unavailable',
              key: 'unavailable',
              render: (v) => (v ? 'Да' : 'Нет'),
            },
          ]}
        />
      ) : (
        <Empty message="Данные ёмкости недоступны" />
      )}
    </Drawer>
  );
}
