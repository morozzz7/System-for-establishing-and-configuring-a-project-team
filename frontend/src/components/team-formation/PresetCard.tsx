/**
 * Preset card component for weight selection.
 * Displays a weight preset with label, description, and mini weight visualization.
 */
import { Card, Tag, Tooltip, Typography, Row, Col } from 'antd';
import type { WeightPreset } from '../../types/reference';
import { METRIC_IDS } from '../../types/reference';

const { Text } = Typography;

interface PresetCardProps {
  preset: WeightPreset;
  selected: boolean;
  onSelect: (presetName: string) => void;
  metrics?: Record<string, { name_ru: string }>;
}

const COLOR_MAP: Record<string, string> = {
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
};

export default function PresetCard({ preset, selected, onSelect, metrics }: PresetCardProps) {
  const weights = preset.weights;

  return (
    <Card
      size="small"
      hoverable
      onClick={() => onSelect(preset.preset)}
      style={{
        borderColor: selected ? '#1677ff' : undefined,
        boxShadow: selected ? '0 0 0 2px #1677ff' : undefined,
        cursor: 'pointer',
        transition: 'all 0.2s',
        height: '100%',
      }}
      title={
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 6, minWidth: 0, width: '100%' }}>
          <Text strong ellipsis style={{ fontSize: 13, minWidth: 0 }}>{preset.label_ru}</Text>
          {selected && (
            <Tag color="blue" style={{ margin: 0, padding: '0 4px', fontSize: 9, lineHeight: '16px', flexShrink: 0, whiteSpace: 'nowrap' }}>
              Выбран
            </Tag>
          )}
        </div>
      }
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 8, marginBottom: 10 }}>
        <Text type="secondary" style={{ fontSize: 12, flex: 1 }}>
          {preset.description_ru}
        </Text>
        <Tooltip title="Сумма весов должна быть 1.0">
          <Text type="secondary" style={{ fontSize: 11, fontFamily: 'monospace', whiteSpace: 'nowrap' }}>
            Σ = {preset.sum_check.toFixed(6)}
          </Text>
        </Tooltip>
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 6px', marginBottom: 12 }}>
        {METRIC_IDS.map((metricId) => {
          const weight = weights[metricId];
          if (weight === undefined || weight === null || weight === 0) return null;
          const pct = (weight * 100).toFixed(0);
          const label = metrics?.[metricId]?.name_ru ?? metricId;
          return (
            <Tooltip key={metricId} title={`${label}: ${pct}%`}>
              <Tag color={COLOR_MAP[metricId] ?? 'default'} style={{ fontSize: 11, margin: 0 }}>
                {metricId} {pct}%
              </Tag>
            </Tooltip>
          );
        })}
      </div>

      <Row gutter={[8, 4]} style={{ fontSize: 11 }}>
        {METRIC_IDS.map((metricId) => {
          const weight = weights[metricId];
          if (weight === undefined || weight === null || weight === 0) return null;
          const maxWeight = Math.max(...Object.values(weights).filter((v): v is number => v !== undefined && v !== null));
          const barWidth = `${(weight / maxWeight) * 100}%`;
          return (
            <Col key={metricId} span={12}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, whiteSpace: 'nowrap' }}>
                <span style={{ width: 24, color: '#8c8c8c', fontSize: 10 }}>{metricId}</span>
                <div style={{ flex: 1, height: 4, background: 'rgba(255, 255, 255, 0.08)', borderRadius: 2, overflow: 'hidden' }}>
                  <div
                    style={{
                      width: barWidth,
                      height: '100%',
                      background: COLOR_MAP[metricId] ?? '#1677ff',
                      borderRadius: 2,
                    }}
                  />
                </div>
                <span style={{ width: 28, textAlign: 'right', fontFamily: 'monospace', fontSize: 10 }}>
                  {(weight * 100).toFixed(0)}%
                </span>
              </div>
            </Col>
          );
        })}
      </Row>
    </Card>
  );
}