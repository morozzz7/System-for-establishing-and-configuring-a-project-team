/**
 * Main weight selection component: preset cards + custom editor + metric definitions panel.
 * Integrates with GET /reference/weight-presets and GET /reference/metrics.
 * Selected preset or custom weights → included in POST /api/v1/runs `weights` field.
 */
import { useCallback, useEffect, useMemo, useState, type ReactElement } from 'react';
import { Tabs, Alert, Space, Card, Divider, Typography, Tag, Row, Col } from 'antd';
import type { Weights, MetricDefinition, WeightPresetListResponse } from '../../types/reference';
import { METRIC_IDS, validateWeights, DEFAULT_WEIGHTS } from '../../types/reference';
import PresetCard from './PresetCard';
import CustomWeightsEditor from './CustomWeightsEditor';
import MetricDefinitionsPanel from './MetricDefinitionsPanel';

const { Text } = Typography;

interface WeightSelectorProps {
  /** Initial preset name (from run config or last selection). */
  initialPreset?: string;
  /** Initial custom weights (if user was in custom mode). */
  initialWeights?: Weights;
  /** Whether project history exists (enables M4). */
  hasProjectHistory?: boolean;
  /** Callback when selection changes (preset or custom weights). */
  onChange: (weights: Weights, preset?: string) => void;
  /** Pre-fetched data (optional, for SSR or parent fetch). */
  presetsData?: WeightPresetListResponse;
  metricsData?: MetricDefinition[];
}

const PRESET_ORDER: Array<'fast' | 'economical' | 'balanced' | 'low_risk'> = [
  'fast',
  'economical',
  'balanced',
  'low_risk',
];

const PRESET_LABELS: Record<string, string> = {
  fast: 'Быстрый',
  economical: 'Экономичный',
  balanced: 'Сбалансированный',
  low_risk: 'Низкий риск',
};

export default function WeightSelector({
  initialPreset = 'balanced',
  initialWeights,
  hasProjectHistory = false,
  onChange,
  presetsData,
  metricsData,
}: WeightSelectorProps) {
  const [mode, setMode] = useState<'preset' | 'custom'>('preset');
  const [activeTab, setActiveTab] = useState<'presets' | 'custom' | 'metrics'>('presets');
  const [selectedPreset, setSelectedPreset] = useState<string>(initialPreset);
  const [customWeights, setCustomWeights] = useState<Weights>(
    initialWeights ?? presetsData?.presets.find((p) => p.preset === initialPreset)?.weights ?? DEFAULT_WEIGHTS,
  );

  // When preset changes in preset mode, update weights and notify
  const handlePresetSelect = useCallback((presetName: string) => {
    setSelectedPreset(presetName);
    setMode('preset');
    setActiveTab('presets');
    const preset = presetsData?.presets.find((p) => p.preset === presetName);
    if (preset) {
      onChange(preset.weights, presetName);
    }
  }, [presetsData, onChange]);

  // When custom weights change, notify
  const handleCustomWeightsChange = (weights: Weights) => {
    setCustomWeights(weights);
    setMode('custom');
    onChange(weights, undefined);
  };

  // Sync mode with activeTab
  useEffect(() => {
    if (activeTab === 'custom') {
      setMode('custom');
    } else if (activeTab === 'presets') {
      setMode('preset');
    }
  }, [activeTab]);

  // Get current effective weights
  const effectiveWeights = useMemo((): Weights => {
    if (mode === 'preset') {
      const preset = presetsData?.presets.find((p) => p.preset === selectedPreset);
      return preset?.weights ?? customWeights;
    }
    return customWeights;
  }, [mode, selectedPreset, customWeights, presetsData]);

  // Validate current weights
  const validation = useMemo(
    () => validateWeights(effectiveWeights, metricsData ?? []),
    [effectiveWeights, metricsData],
  );

  // Render preset cards as a responsive grid of cards (not nested tabs).
  const presetCards = useMemo((): ReactElement[] | null => {
    if (!presetsData) return null;
    const cards: ReactElement[] = [];
    for (const presetName of PRESET_ORDER) {
      const preset = presetsData.presets.find((p) => p.preset === presetName);
      if (!preset) continue;
      cards.push(
        <Col key={presetName} xs={24} sm={12} lg={6}>
          <PresetCard
            preset={preset}
            selected={mode === 'preset' && selectedPreset === presetName}
            onSelect={handlePresetSelect}
            metrics={presetsData.metrics}
          />
        </Col>,
      );
    }
    return cards;
  }, [presetsData, mode, selectedPreset, handlePresetSelect]);

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space wrap>
        <Text strong>Критерии оптимизации</Text>
        <Text type="secondary" style={{ fontSize: 12 }}>
          Выберите пресет или настройте веса вручную
        </Text>
      </Space>

      <Alert
        type={validation.valid ? 'success' : 'warning'}
        showIcon
        message={
          mode === 'preset'
            ? `Выбран пресет: ${PRESET_LABELS[selectedPreset] ?? selectedPreset}`
            : 'Режим пользовательских весов'
        }
        description={
          <Space direction="vertical" size={0} style={{ width: '100%' }}>
            <Text>
              Текущая сумма весов:{' '}
              <Text strong style={{ fontFamily: 'monospace' }}>{validation.sum.toFixed(6)}</Text>
              {' '}
              {validation.valid ? '✓' : '✗ (нужно 1.000000 ± 0.000001)'}
            </Text>
            {(mode === 'custom' || activeTab === 'custom') && !hasProjectHistory && (
              <Text type="warning" style={{ fontSize: 12 }}>
                ⚠ M4 (Релевантность опыта) будет исключена — нет истории проектов. Веса перераспределятся пропорционально.
              </Text>
            )}
          </Space>
        }
      />

      <Tabs activeKey={activeTab} onChange={(key) => setActiveTab(key as 'presets' | 'custom' | 'metrics')} style={{ width: '100%' }}>
        <Tabs.TabPane tab="Пресеты" key="presets">
          {presetCards && presetCards.length > 0 ? (
            <Row gutter={[16, 16]}>
              {presetCards}
            </Row>
          ) : (
            <Alert type="info" message="Загрузка пресетов..." showIcon />
          )}
        </Tabs.TabPane>

        <Tabs.TabPane tab="Настроить веса" key="custom">
          <CustomWeightsEditor
            initialWeights={customWeights}
            metricDefinitions={metricsData ?? []}
            hasProjectHistory={hasProjectHistory}
            onChange={handleCustomWeightsChange}
            onReset={() => {
              const balanced = presetsData?.presets.find((p) => p.preset === 'balanced');
              if (balanced) {
                setSelectedPreset('balanced');
                setMode('preset');
                onChange(balanced.weights, 'balanced');
              }
            }}
          />
        </Tabs.TabPane>

        <Tabs.TabPane tab="Справочник метрик" key="metrics">
          <MetricDefinitionsPanel metrics={metricsData ?? []} />
        </Tabs.TabPane>
      </Tabs>

      <Divider style={{ margin: '8px 0' }} />

      <Card type="inner" size="small" style={{ width: '100%' }}>
        <Space direction="vertical" size={4} style={{ width: '100%' }}>
          <Text strong>Текущие веса (для POST /runs):</Text>
          <Space wrap size={[4, 4]}>
            {METRIC_IDS.map((metricId) => {
              const weight = effectiveWeights[metricId];
              if (weight === undefined || weight === null || weight === 0) return null;
              return (
                <Tag key={metricId} color="blue">
                  {metricId}: {(weight * 100).toFixed(1)}%
                </Tag>
              );
            })}
          </Space>
          <Text type="secondary" style={{ fontSize: 11, fontFamily: 'monospace' }}>
            {JSON.stringify(effectiveWeights, null, 2)}
          </Text>
        </Space>
      </Card>
    </Space>
  );
}