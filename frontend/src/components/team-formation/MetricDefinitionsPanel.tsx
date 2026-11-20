/**
 * Collapsible panel showing full metric definitions from GET /reference/metrics.
 * Table: ID, Name, Formula, Interpretation, Core/Optional badge.
 */
import { Collapse, Table, Tag, Typography, Space, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { MetricDefinition } from '../../types/reference';

const { Text } = Typography;

interface MetricDefinitionsPanelProps {
  metrics: MetricDefinition[];
}

const COLORS: Record<string, string> = {
  M1: 'blue',
  M2: 'cyan',
  M3: 'geekblue',
  M4: 'purple',
  M5: 'green',
  M6: 'gold',
  M7: 'lime',
  M8: 'orange',
  M9: 'red',
  M10: 'volcano',
  M11: 'magenta',
  M12: 'grey',
  M13: 'grey',
};

export default function MetricDefinitionsPanel({ metrics }: MetricDefinitionsPanelProps) {
  const columns: ColumnsType<MetricDefinition> = [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 60,
      render: (id) => (
        <Tag color={COLORS[id] ?? 'default'} style={{ fontWeight: 600 }}>
          {id}
        </Tag>
      ),
    },
    {
      title: 'Название',
      dataIndex: 'name_ru',
      key: 'name_ru',
      width: 180,
      render: (name, record) => (
        <Space direction="vertical" size={0} style={{ width: '100%' }}>
          <Text strong>{name}</Text>
          <Text type="secondary" style={{ fontSize: 11 }}>
            {record.name_en}
          </Text>
        </Space>
      ),
    },
    {
      title: 'Формула',
      dataIndex: 'formula_ru',
      key: 'formula_ru',
      width: 280,
      render: (formula) => (
        <Text type="secondary" style={{ fontFamily: 'monospace', fontSize: 12 }}>
          {formula}
        </Text>
      ),
    },
    {
      title: 'Интерпретация',
      dataIndex: 'interpretation_ru',
      key: 'interpretation_ru',
      render: (interpretation) => <Text style={{ fontSize: 13 }}>{interpretation}</Text>,
    },
    {
      title: 'Тип',
      dataIndex: 'is_core',
      key: 'is_core',
      width: 100,
      render: (isCore) => (
        <Tag color={isCore ? 'green' : 'orange'}>
          {isCore ? 'Основная' : 'Опциональная'}
        </Tag>
      ),
    },
    {
      title: 'Типичный диапазон веса',
      dataIndex: 'typical_weight_range',
      key: 'typical_weight_range',
      width: 160,
      render: (range) => (
        <Space wrap size={[4, 4]}>
          <Tooltip title="Минимальный типичный вес">
            <Tag color="blue" style={{ fontSize: 11 }}>
              {(range[0] * 100).toFixed(0)}%
            </Tag>
          </Tooltip>
          <Text type="secondary">–</Text>
          <Tooltip title="Максимальный типичный вес">
            <Tag color="blue" style={{ fontSize: 11 }}>
              {(range[1] * 100).toFixed(0)}%
            </Tag>
          </Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <Collapse
      defaultActiveKey={[]}
      bordered
      expandIconPosition="right"
      style={{ width: '100%' }}
    >
      <Collapse.Panel
        header={
          <Space>
            <Text strong>Справочник метрик (M1–M13)</Text>
            <Tag color="blue">{metrics.length}</Tag>
          </Space>
        }
        key="metrics"
        extra={<Text type="secondary" style={{ fontSize: 12 }}>Нажмите для разворачивания</Text>}
      >
        <Table
          dataSource={metrics}
          columns={columns}
          rowKey="id"
          pagination={false}
          size="small"
          bordered
        />
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
          <Text strong>Основные метрики (Core):</Text> включаются в Score всегда.{' '}
          <Text strong>Опциональные:</Text> M4 (требует истории проектов), M13 (экспериментальная).
          При отсутствии истории вес M4 переносится на остальные пропорционально (METRICS.md §1).
        </Text>
      </Collapse.Panel>
    </Collapse>
  );
}