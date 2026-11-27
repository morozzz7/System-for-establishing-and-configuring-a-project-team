import { formatWeekDate } from '../utils/date';
/**
 * Run configuration page: shown before creating a new optimization run.
 * Allows selecting weight preset / custom weights, and other run parameters.
 * Submits POST /api/v1/runs with the selected weights.
 */
import { useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Alert,
  Button,
  Card,
  Form,
  InputNumber,
  Radio,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd';
import type { Brief, TeamRun, Horizon, PresetName, Weights } from '../types';
import { DEFAULT_WEIGHTS } from '../types/reference';
import { createRun, getBrief, listBriefs } from '../api/client';
import { ApiError, toApiError } from '../api/errors';
import { ensureToken } from '../api/auth';
import WeightSelector from '../components/team-formation/WeightSelector';
import { useWeightPresetsWithMetrics, useMetricDefinitions } from '../hooks';

const { Title, Text, Paragraph } = Typography;

const WEEK_OPTIONS = Array.from({ length: 52 }, (_, i) => `2026-W${String(i + 1).padStart(2, '0')}`);

interface RunConfigPageProps {
  /** Optional briefId from URL params. */
  briefId?: string;
}

export default function RunConfigPage({ briefId: propBriefId }: RunConfigPageProps) {
  const { briefId: paramBriefId } = useParams<{ briefId?: string }>();
  const navigate = useNavigate();

  const briefId = propBriefId ?? paramBriefId;
  const [brief, setBrief] = useState<Brief | null>(null);
  const [briefs, setBriefs] = useState<Brief[]>([]);
  const [selectedBriefId, setSelectedBriefId] = useState<string | undefined>(briefId);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [runParams, setRunParams] = useState({
    k: 2,
    seed: 42,
    time_limit_ms: 10000,
    // Null means "derive the window from the brief" (contract RunRequest.horizon is optional).
    horizon: null as Horizon | null,
    candidate_filter: {
      include_bench: true,
      include_external: false,
      max_candidates: 500,
    },
  });
  const [selectedWeights, setSelectedWeights] = useState<Weights | null>(DEFAULT_WEIGHTS);
  const [selectedPreset, setSelectedPreset] = useState<PresetName | undefined>('balanced');

  const presets = useWeightPresetsWithMetrics();
  const metrics = useMetricDefinitions();

  // Synchronize weights with loaded preset if using preset mode
  useEffect(() => {
    if (presets.data?.presets && selectedPreset) {
      const p = presets.data.presets.find(
        (x) => (x.preset || x.Preset) === selectedPreset,
      );
      if (p) {
        const w = p.weights || p.Weights;
        if (w) setSelectedWeights(w);
      }
    }
  }, [presets.data, selectedPreset]);

  // Load briefs list
  useEffect(() => {
    let cancelled = false;
    listBriefs()
      .then((data) => {
        if (!cancelled) {
          setBriefs(data);
          if (briefId && data.some((b) => b.brief_id === briefId)) {
            setSelectedBriefId(briefId);
          } else if (data.length > 0 && !selectedBriefId) {
            setSelectedBriefId(data[0].brief_id);
          }
        }
      })
      .catch((err) => {
        if (!cancelled) setError(toApiError(err));
      });
    return () => {
      cancelled = true;
    };
  }, [briefId, selectedBriefId]);

  // Load selected brief details
  useEffect(() => {
    if (!selectedBriefId) {
      setBrief(null);
      return;
    }
    let cancelled = false;
    getBrief(selectedBriefId)
      .then((data: Brief | null) => {
        if (!cancelled) setBrief(data);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(toApiError(err));
      });
    return () => {
      cancelled = true;
    };
  }, [selectedBriefId]);

  const initializedBriefRef = useRef<string | null>(null);
  useEffect(() => {
    if (!brief || initializedBriefRef.current === brief.brief_id) return;
    initializedBriefRef.current = brief.brief_id;
    if (brief.requested_start_week && brief.deadline_week) {
      setRunParams((params) => ({
        ...params,
        horizon: {
          start_week: brief.requested_start_week!,
          end_week: brief.deadline_week!,
        },
      }));
    }
  }, [brief]);

  const handleWeightsChange = (weights: Weights, preset?: string) => {
    setSelectedWeights(weights);
    setSelectedPreset(preset as PresetName | undefined);
  };

  const handleSubmit = async () => {
    if (!selectedBriefId) {
      setError(
        new ApiError({
          status: 400,
          code: 'VALIDATION_ERROR',
          detail: 'Выберите бриф',
          reasonCodes: [],
        }),
      );
      return;
    }
    if (!brief || brief.status !== 'confirmed') {
      setError(
        new ApiError({
          status: 409,
          code: 'CONFLICT_BRIEF_STATE',
          detail: 'Бриф должен быть в статусе "confirmed"',
          reasonCodes: ['BRIEF_NOT_CONFIRMED'],
        }),
      );
      return;
    }
    if (!selectedWeights) {
      setError(
        new ApiError({
          status: 400,
          code: 'VALIDATION_ERROR',
          detail: 'Веса не выбраны',
          reasonCodes: [],
        }),
      );
      return;
    }

    setCreating(true);
    setError(null);

    try {
      const API_BASE = import.meta.env.VITE_API_BASE_URL as string | undefined;
      if (API_BASE) {
        const token = await ensureToken();
        const response = await fetch(`${API_BASE}/runs`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Accept: 'application/json',
            'X-Request-Id': `req_ui${Date.now()}`,
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({
            brief_id: selectedBriefId,
            preset: selectedPreset ?? 'balanced',
            weights: selectedWeights,
            k: runParams.k,
            seed: runParams.seed,
            time_limit_ms: runParams.time_limit_ms,
            ...(runParams.horizon?.start_week && runParams.horizon?.end_week
              ? { horizon: runParams.horizon }
              : {}),
            candidate_filter: runParams.candidate_filter,
          }),
        });
        if (!response.ok) {
          const problem = await response.json();
          throw new ApiError({
            status: response.status,
            code: problem.code ?? `HTTP_${response.status}`,
            detail: problem.detail ?? 'Ошибка создания запуска',
            reasonCodes: problem.reason_codes ?? [],
          });
        }
        const newRun = (await response.json()) as TeamRun;
        navigate(`/runs/new/${selectedBriefId}?run=${newRun.run_id}`);
      } else {
        // Mock mode: just navigate with the created mock run
        const mockRun = await createRun(selectedBriefId);
        navigate(`/runs/new/${selectedBriefId}?run=${mockRun.run_id}`);
      }
    } catch (err) {
      setError(toApiError(err));
    } finally {
      setCreating(false);
    }
  };

  if (presets.loading || metrics.loading) {
    return <Spin tip="Загрузка пресетов и метрик..." size="large" />;
  }
  if (presets.error || metrics.error) {
    return <Alert type="error" message="Ошибка загрузки" description={presets.error ?? metrics.error} showIcon />;
  }

  const hasProjectHistory = false; // TODO: determine from brief or project history

  return (
    <Space direction="vertical" size="large" style={{ width: '100%', maxWidth: 900, margin: '0 auto' }}>
      <Title level={2}>Настройка расчёта вариантов</Title>

      <Paragraph>
        Выберите бриф, пресет весов или настройте веса вручную, затем запустите расчёт.
        Система построит 2–3 варианта команды с оценками длительности, стоимости и дефицитов.
      </Paragraph>

      {error && (
        <Alert
          type="error"
          showIcon
          message={`Ошибка: ${error.code}`}
          description={
            <Space direction="vertical" size={0}>
              <Text>{error.detail}</Text>
              {error.reasonCodes.length > 0 && (
                <Space wrap size={[4, 4]}>
                  {error.reasonCodes.map((code) => (
                    <Tag key={code} color="red">
                      {code}
                    </Tag>
                  ))}
                </Space>
              )}
            </Space>
          }
        />
      )}

      <Card title="Бриф">
        <Form layout="vertical" style={{ width: '100%' }}>
          <Form.Item label="Бриф" name="briefId">
            <Select
              value={selectedBriefId}
              onChange={setSelectedBriefId}
              style={{ width: '100%' }}
              showSearch
              placeholder="Выберите бриф"
              options={briefs.map((b) => ({
                value: b.brief_id,
                label: `${b.title} (${b.brief_id}) — ${b.status}`,
              }))}
              allowClear
            />
          </Form.Item>
          {brief && (
            <Space direction="vertical" size="small" style={{ width: '100%', marginTop: 8 }}>
              <Text type="secondary">Статус: <Text strong>{brief.status}</Text></Text>
              <Text type="secondary">
                Требований: <Text strong>{brief.requirement_count}</Text>{' '}
                {brief.needs_review_count > 0 && (
                  <Tag color="orange">Требуют ревью: {brief.needs_review_count}</Tag>
                )}
              </Text>
              <Text type="secondary">
                Срок: <Text strong>{brief.requested_start_week ? formatWeekDate(brief.requested_start_week) : '—'}</Text>{' '}–{' '}
                <Text strong>{brief.deadline_week ? formatWeekDate(brief.deadline_week, true) : '—'}</Text>
              </Text>
            </Space>
          )}
        </Form>
      </Card>

      <Card title="Параметры запуска">
        <Form layout="vertical" style={{ width: '100%' }}>
          <Form.Item label="Количество вариантов (k)" name="k">
            <InputNumber min={1} max={5} value={runParams.k} onChange={(v) => setRunParams({ ...runParams, k: v ?? 2 })} />
          </Form.Item>
          <Form.Item label="Seed (детерминизм)" name="seed">
            <InputNumber min={0} max={1000000} value={runParams.seed} onChange={(v) => setRunParams({ ...runParams, seed: v ?? 42 })} />
          </Form.Item>
          <Form.Item label="Лимит времени (мс)" name="time_limit_ms">
            <InputNumber min={1000} max={60000} step={1000} value={runParams.time_limit_ms} onChange={(v) => setRunParams({ ...runParams, time_limit_ms: v ?? 10000 })} />
          </Form.Item>
          <Form.Item label="Горизонт планирования" name="horizon">
            <Space wrap>
              <Select
                data-testid="horizon-start-week"
                showSearch
                style={{ width: 140 }}
                value={runParams.horizon?.start_week}
                onChange={(v) =>
                  setRunParams((params) => ({
                    ...params,
                    horizon: { start_week: v, end_week: params.horizon?.end_week ?? '' },
                  }))
                }
                placeholder="Старт"
                options={WEEK_OPTIONS.map((w) => ({ value: w, label: w }))}
              />
              <Text>–</Text>
              <Select
                data-testid="horizon-end-week"
                showSearch
                style={{ width: 140 }}
                value={runParams.horizon?.end_week}
                onChange={(v) =>
                  setRunParams((params) => ({
                    ...params,
                    horizon: { start_week: params.horizon?.start_week ?? '', end_week: v },
                  }))
                }
                placeholder="Конец"
                options={WEEK_OPTIONS.map((w) => ({ value: w, label: w }))}
              />
            </Space>
          </Form.Item>
          <Form.Item label="Кандидаты" name="candidate_filter">
            <Space direction="vertical" size={8}>
              <Radio.Group
                value={runParams.candidate_filter.include_bench ? 'bench' : 'no_bench'}
                onChange={(e) => setRunParams({ ...runParams, candidate_filter: { ...runParams.candidate_filter, include_bench: e.target.value === 'bench' } })}
              >
                <Radio.Button value="bench">Включать bench</Radio.Button>
                <Radio.Button value="no_bench">Только в проектах</Radio.Button>
              </Radio.Group>
              <Radio.Group
                value={runParams.candidate_filter.include_external ? 'external' : 'no_external'}
                onChange={(e) => setRunParams({ ...runParams, candidate_filter: { ...runParams.candidate_filter, include_external: e.target.value === 'external' } })}
              >
                <Radio.Button value="external">Разрешить внешних</Radio.Button>
                <Radio.Button value="no_external">Только внутренние</Radio.Button>
              </Radio.Group>
              <InputNumber
                min={10}
                max={1000}
                step={10}
                value={runParams.candidate_filter.max_candidates}
                onChange={(v) => setRunParams({ ...runParams, candidate_filter: { ...runParams.candidate_filter, max_candidates: v ?? 500 } })}
                placeholder="Макс. кандидатов"
                style={{ width: 180 }}
              />
            </Space>
          </Form.Item>
        </Form>
      </Card>

      <WeightSelector
        presetsData={presets.data ?? undefined}
        metricsData={metrics.data ?? undefined}
        initialPreset={selectedPreset}
        initialWeights={selectedWeights ?? undefined}
        hasProjectHistory={hasProjectHistory}
        onChange={handleWeightsChange}
      />

      <Space wrap style={{ width: '100%', justifyContent: 'flex-end' }}>
        <Button onClick={() => navigate(-1)} disabled={creating}>
          Назад
        </Button>
        <Button type="primary" onClick={handleSubmit} loading={creating} disabled={!selectedBriefId || !brief || brief.status !== 'confirmed'}>
          {creating ? 'Запуск...' : 'Рассчитать варианты'}
        </Button>
      </Space>

      <Paragraph type="secondary" style={{ fontSize: 12 }}>
        После запуска будет создан новый run (GET /runs/{"{runId}"}) с вариантами. История изменений доступна в аудите.
      </Paragraph>
    </Space>
  );
}