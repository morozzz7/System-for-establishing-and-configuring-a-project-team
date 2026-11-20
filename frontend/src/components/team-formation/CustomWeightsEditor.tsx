/**
 * Custom weights editor with sliders/inputs for M1-M12.
 * Live validation: sum=1.0 ±1e-6, typical range warnings.
 * Shows metric names and descriptions as tooltips.
 */
import { useMemo, useState } from 'react';
import {
  Slider,
  InputNumber,
  Tooltip,
  Typography,
  Alert,
  Space,
  Button,
  Row,
  Col,
  Divider,
  Tag,
} from 'antd';
import type { Weights, MetricDefinition } from '../../types/reference';
import { METRIC_IDS, OPTIONAL_METRIC_IDS, validateWeights, normalizeWeights, DEFAULT_WEIGHTS } from '../../types/reference';

const { Text } = Typography;

interface CustomWeightsEditorProps {
  initialWeights: Weights;
  metricDefinitions: MetricDefinition[];
  hasProjectHistory: boolean;
  onChange: (weights: Weights) => void;
  onReset?: () => void;
}

const METRIC_LABELS: Record<string, string> = {
  M1: 'Покрытие компетенций',
  M2: 'Покрытие трудозатрат',
  M3: 'Соответствие уровней',
  M4: 'Релевантность опыта',
  M5: 'График',
  M6: 'Стоимость',
  M7: 'Здоровье нагрузки',
  M8: 'Влияние на портфель',
  M9: 'Bus-factor',
  M10: 'Фокус',
  M11: 'Использование бенча',
  M12: 'Устойчивость',
};

export default function CustomWeightsEditor({
  initialWeights,
  metricDefinitions,
  hasProjectHistory,
  onChange,
  onReset,
}: CustomWeightsEditorProps) {
  const [weights, setWeights] = useState<Weights>(initialWeights);

  const validation = useMemo(
    () => validateWeights(weights, metricDefinitions),
    [weights, metricDefinitions],
  );

  const defMap = useMemo(
    () => new Map(metricDefinitions.map((d) => [d.id, d])),
    [metricDefinitions],
  );

  const handleWeightChange = (metricId: string, value: number) => {
    const newWeights = { ...weights, [metricId]: value };
    setWeights(newWeights);
    onChange(normalizeWeights(newWeights));
  };

  const handleSliderChange = (metricId: string, value: number | number[]) => {
    const numValue = Array.isArray(value) ? value[0] : value;
    handleWeightChange(metricId, numValue / 100);
  };

  const handleInputChange = (metricId: string, value: number | undefined) => {
    if (value === undefined) return;
    handleWeightChange(metricId, Math.max(0, Math.min(1, value)));
  };

  const handleReset = () => {
    const normalized = normalizeWeights(DEFAULT_WEIGHTS);
    setWeights(normalized);
    onChange(normalized);
    onReset?.();
  };

  const optionalMetric = OPTIONAL_METRIC_IDS[0];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space wrap>
        <Text strong>Пользовательские веса</Text>
        {onReset && (
          <Button type="text" size="small" onClick={handleReset}>
            Сбросить на «Сбалансированный»
          </Button>
        )}
      </Space>

      <Alert
        type={validation.valid ? 'success' : 'error'}
        showIcon
        message={validation.valid ? 'Веса валидны' : 'Веса невалидны'}
        description={
          <Space direction="vertical" size={0} style={{ width: '100%' }}>
            <Text>
              Сумма: <Text strong style={{ fontFamily: 'monospace' }}>{validation.sum.toFixed(6)}</Text>{' '}
              (нужно 1.000000 ± 0.000001)
            </Text>
            {validation.errors.length > 0 && (
              <ul style={{ margin: '4px 0 0 18px', padding: 0, color: '#ff4d4f' }}>
                {validation.errors.map((e, i) => (
                  <li key={i}>{e}</li>
                ))}
              </ul>
            )}
            {validation.warnings.length > 0 && (
              <ul style={{ margin: '4px 0 0 18px', padding: 0, color: '#faad14' }}>
                {validation.warnings.map((w, i) => (
                  <li key={i}>{w}</li>
                ))}
              </ul>
            )}
          </Space>
        }
      />

      <Divider style={{ margin: '8px 0' }} />

      <Row gutter={[16, 12]} style={{ width: '100%' }}>
        {METRIC_IDS.map((metricId) => {
          const isOptional = metricId === optionalMetric;
          const disabled = isOptional && !hasProjectHistory;
          const def = defMap.get(metricId);
          const label = METRIC_LABELS[metricId] ?? metricId;
          const description = def?.interpretation_ru ?? '';
          const typicalRange = def?.typical_weight_range ?? [0, 1];
          const weight = weights[metricId] ?? 0;
          const pct = Math.round(weight * 100);

          return (
            <Col
              key={metricId}
              xs={24}
              sm={12}
              md={8}
              lg={6}
              style={{
                opacity: disabled ? 0.5 : 1,
                transition: 'opacity 0.2s',
              }}
            >
              <Tooltip title={description} placement="top">
                <Space direction="vertical" size={4} style={{ width: '100%' }}>
                  <Space wrap style={{ width: '100%' }}>
                    <Text strong>{metricId}</Text>
                    <Text type="secondary" style={{ flex: 1, textAlign: 'right' }}>
                      {pct}%
                    </Text>
                  </Space>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {label}
                  </Text>

                  <Slider
                    min={0}
                    max={100}
                    step={1}
                    value={pct}
                    onChange={(val) => handleSliderChange(metricId, val)}
                    disabled={disabled}
                    tooltip={{ formatter: (v) => `${v}%` }}
                    marks={{
                      0: '0%',
                      25: '25%',
                      50: '50%',
                      75: '75%',
                      100: '100%',
                    }}
                  />

                  <Space wrap size={[8, 4]} style={{ width: '100%' }}>
                    <InputNumber
                      min={0}
                      max={100}
                      step={1}
                      value={pct}
                      onChange={(val) => handleInputChange(metricId, (val ?? 0) / 100)}
                      disabled={disabled}
                      style={{ width: 80 }}
                      addonAfter="%"
                      precision={0}
                    />
                    <Tooltip
                      title={`Типичный диапазон: ${(typicalRange[0] * 100).toFixed(0)}% – ${(typicalRange[1] * 100).toFixed(0)}%`}
                      placement="top"
                    >
                      <Text type="secondary" style={{ fontSize: 11, fontFamily: 'monospace' }}>
                        [{ (typicalRange[0] * 100).toFixed(0)}% – {(typicalRange[1] * 100).toFixed(0)}%]
                      </Text>
                    </Tooltip>
                    {isOptional && !hasProjectHistory && (
                      <Tag color="orange" style={{ fontSize: 10 }}>
                        опционально (нет истории)
                      </Tag>
                    )}
                  </Space>
                </Space>
              </Tooltip>
            </Col>
          );
        })}
      </Row>

      <Divider style={{ margin: '8px 0' }} />

      <Space wrap>
        <Text type="secondary" style={{ fontSize: 12 }}>
          M4 (Релевантность опыта) — исключается из Score при отсутствии истории проектов; веса перераспределяются пропорционально (METRICS §1).
        </Text>
      </Space>
    </Space>
  );
}