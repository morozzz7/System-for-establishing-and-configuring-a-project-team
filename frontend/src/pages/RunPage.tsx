/**
 * Team variants screen: compares variants, exposes the constraint editor and shows the
 * recalculation result (applied / rejected overrides + measured facts).
 *
 * Human-in-the-loop: the system only proposes; the manager picks the variant and edits constraints.
 * Every number shown here comes from the API (Go is the system of record, ADR-007 §1).
 */
import { useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Empty as AntEmpty,
  Input,
  List,
  Modal,
  Radio,
  Row,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type {
  DecisionResponse,
  Explanation,
  RecalculateResponse,
  RejectedOverride,
  RunInputs,
  TeamRun,
  Variant,
  VariantSummary,
} from '../types';
import { ApiError, reasonCodeLabel, toApiError } from '../api/errors';
import {
  createRun,
  getRun,
  getRunInputs,
  getVariant,
  getVariantExplanation,
  listRoles,
  recordDecision,
} from '../api/client';
import ConstraintEditor from '../components/ConstraintEditor';
import ExplanationPanel, { ExplanationLoading } from '../components/ExplanationPanel';
import { GradeTag } from '../components/shared';
import { formatDeficits, formatRatio, formatUnits, formatWeeks, factNumber, KEY_METRICS } from '../utils/metrics';

const { Title, Text, Paragraph } = Typography;

const STATUS_COLORS: Record<string, string> = {
  queued: 'default',
  running: 'processing',
  succeeded: 'green',
  partial: 'orange',
  failed: 'red',
};

function VariantCard({
  variant,
  selected,
  onSelect,
}: {
  variant: VariantSummary;
  selected: boolean;
  onSelect: (variantId: string) => void;
}) {
  return (
    <Card
      size="small"
      hoverable
      onClick={() => onSelect(variant.variant_id)}
      style={selected ? { borderColor: '#1677ff' } : undefined}
      title={
        <Space wrap>
          <Radio value={variant.variant_id} aria-label={`Выбрать вариант ${variant.label}`}>
            {variant.label}
          </Radio>
          {!variant.gate_pass && <Tag color="red">жёсткие ограничения нарушены</Tag>}
        </Space>
      }
    >
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Space wrap size={[8, 4]}>
          <Tag>Состав: {variant.team_size}</Tag>
          <Tooltip title="Диапазон длительности: P50 и P80 (Monte Carlo, фиксированное зерно)">
            <Tag color="blue">
              P50 {formatWeeks(variant.duration.p50_weeks)} · P80 {formatWeeks(variant.duration.p80_weeks)}
            </Tag>
          </Tooltip>
          <Tag color="geekblue">Балл {formatRatio(variant.score)}</Tag>
          <Tag>{formatUnits(variant.cost_units)}</Tag>
          <Tag color={variant.deficit_count > 0 ? 'orange' : 'default'}>{formatDeficits(variant.deficit_count)}</Tag>
          {variant.duration.deadline_week && (
            <Tag color="purple">
              срок {variant.duration.deadline_week} · P(в срок) {formatRatio(variant.duration.p_on_time)}
            </Tag>
          )}
        </Space>

        <Row gutter={[8, 4]}>
          {KEY_METRICS.map((metric) => (
            <Col key={metric.code} xs={12} sm={8}>
              <Tooltip title={`${metric.code}: ${metric.hint}`}>
                <Space direction="vertical" size={0}>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {metric.code} · {metric.label}
                  </Text>
                  <Text strong>{formatRatio(variant.metric_values[metric.key])}</Text>
                </Space>
              </Tooltip>
            </Col>
          ))}
        </Row>

        <Space wrap size={[4, 4]}>
          {variant.reason_codes.map((code) => (
            <Tag key={code} color="default">
              {reasonCodeLabel(code)}
            </Tag>
          ))}
        </Space>
      </Space>
    </Card>
  );
}

function RejectedOverridesList({ rejected }: { rejected: RejectedOverride[] }) {
  return (
    <List
      size="small"
      dataSource={rejected}
      renderItem={(item) => (
        <List.Item>
          <Space direction="vertical" size={0}>
            <Text>{`Ограничение ${item.index + 1}: отклонено сервером`}</Text>
            <Space wrap size={[4, 4]}>
              {item.reason_codes.map((code) => (
                <Tag key={code} color="red">
                  {code}
                </Tag>
              ))}
            </Space>
            {item.detail && (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {item.detail}
              </Text>
            )}
          </Space>
        </List.Item>
      )}
    />
  );
}

function RecalculationResult({ result }: { result: RecalculateResponse }) {
  const noEffect = result.reason_codes.includes('RECALC_NO_EFFECT');
  const changedMembers = factNumber(result.facts, 'changed_members');
  const rosterJaccard = factNumber(result.facts, 'roster_jaccard');
  const p50Before = factNumber(result.facts, 'p50_weeks_before');
  const p50After = factNumber(result.facts, 'p50_weeks_after');

  return (
    <Card
      size="small"
      data-testid="recalc-result"
      title="Результат пересчёта"
      extra={<Tag color={noEffect ? 'orange' : 'green'}>{result.reason_codes.join(', ')}</Tag>}
    >
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Alert
          type={noEffect ? 'warning' : 'success'}
          showIcon
          message={noEffect ? 'Изменений нет (RECALC_NO_EFFECT)' : 'Пересчёт применён (RECALC_APPLIED)'}
          description={
            noEffect
              ? 'Заданные ограничения не изменили состав и оценку. Ниже — измеренные факты, а не предположение.'
              : 'Создан новый запуск. Исходный запуск и его снимок входа не изменились.'
          }
        />

        <Descriptions size="small" column={2} bordered>
          <Descriptions.Item label="Применено ограничений">{result.applied_overrides}</Descriptions.Item>
          <Descriptions.Item label="Отклонено ограничений">{result.rejected_overrides.length}</Descriptions.Item>
          <Descriptions.Item label="Изменилось участников">{changedMembers ?? '—'}</Descriptions.Item>
          <Descriptions.Item label="Расхождение составов (Жаккар)">{rosterJaccard ?? '—'}</Descriptions.Item>
          <Descriptions.Item label="P50 до">{p50Before === undefined ? '—' : formatWeeks(p50Before)}</Descriptions.Item>
          <Descriptions.Item label="P50 после">{p50After === undefined ? '—' : formatWeeks(p50After)}</Descriptions.Item>
        </Descriptions>

        <Space wrap size={[4, 4]}>
          {result.reason_codes.map((code) => (
            <Tag key={code}>{reasonCodeLabel(code)}</Tag>
          ))}
        </Space>

        {result.rejected_overrides.length > 0 && (
          <Card size="small" type="inner" title="Отклонённые ограничения">
            <RejectedOverridesList rejected={result.rejected_overrides} />
          </Card>
        )}
      </Space>
    </Card>
  );
}

function VariantDetailCard({
  variant,
  roleNames,
}: {
  variant: Variant;
  roleNames: Record<string, string>;
}) {
  return (
    <Card size="small" title={`Состав варианта: ${variant.label}`}>
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <List
          size="small"
          dataSource={variant.members}
          locale={{ emptyText: 'Участники не определены' }}
          renderItem={(member) => (
            <List.Item data-testid="variant-member">
              <Space wrap>
                <Text strong data-testid="variant-member-id">{member.employee_id}</Text>
                <Tag>{roleNames[member.role_id] ?? member.role_id}</Tag>
                <GradeTag grade={member.grade} />
                <Text type="secondary">{member.allocated_hours} ч</Text>
                <Tooltip title="Соответствие требуемому уровню компетенции">
                  <Text type="secondary">соответствие {formatRatio(member.competence_fit)}</Text>
                </Tooltip>
                {member.reason_codes.map((code) => (
                  <Tag key={code} color="blue">
                    {code}
                  </Tag>
                ))}
              </Space>
            </List.Item>
          )}
        />

        <Divider style={{ margin: '4px 0' }} />

        <Text strong>Дефициты компетенций</Text>
        {variant.deficits.length === 0 ? (
          <Text type="secondary">Дефицитов нет.</Text>
        ) : (
          <List
            size="small"
            dataSource={variant.deficits}
            renderItem={(deficit) => (
              <List.Item>
                <Space direction="vertical" size={0}>
                  <Space wrap>
                    <Text>{deficit.competency_name_ru}</Text>
                    <Tag color="orange">{deficit.person_days} чел.-дн.</Tag>
                    {deficit.external_cooperation_flag && <Tag color="red">нужна внешняя кооперация</Tag>}
                  </Space>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {deficit.recommended_actions.map((action) => `${action.action}: ${action.reason_codes.join(', ')}`).join('; ')}
                  </Text>
                </Space>
              </List.Item>
            )}
          />
        )}
      </Space>
    </Card>
  );
}

export default function RunPage() {
  const { briefId = '' } = useParams();
  const [searchParams] = useSearchParams();
  const requestedRunId = searchParams.get('run');

  const [run, setRun] = useState<TeamRun | null>(null);
  const [inputs, setInputs] = useState<RunInputs | null>(null);
  const [inputsError, setInputsError] = useState<string | null>(null);
  const [selectedVariantId, setSelectedVariantId] = useState<string | undefined>(undefined);
  const [variantDetail, setVariantDetail] = useState<Variant | null>(null);
  const [explanation, setExplanation] = useState<Explanation | null>(null);
  const [explanationLoading, setExplanationLoading] = useState(false);
  const [roleNames, setRoleNames] = useState<Record<string, string>>({});
  const [pageError, setPageError] = useState<ApiError | null>(null);
  const [recalcResult, setRecalcResult] = useState<RecalculateResponse | null>(null);
  const [recalcError, setRecalcError] = useState<ApiError | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [loadingRun, setLoadingRun] = useState(false);
  const [creating, setCreating] = useState(false);
  const [decisionModalOpen, setDecisionModalOpen] = useState(false);
  const [decisionComment, setDecisionComment] = useState('');
  const [deciding, setDeciding] = useState(false);
  const [decision, setDecision] = useState<DecisionResponse | null>(null);
  const [decisionError, setDecisionError] = useState<ApiError | null>(null);

  const runId = run?.run_id;

  // Deep link: /runs/new/:briefId?run=<runId> (e.g. the parent of a recalculation run).
  useEffect(() => {
    if (!requestedRunId) return;
    let cancelled = false;
    setLoadingRun(true);
    getRun(requestedRunId)
      .then((loaded) => {
        if (!cancelled && loaded) setRun(loaded);
      })
      .catch((error: unknown) => {
        if (!cancelled) setPageError(toApiError(error));
      })
      .finally(() => {
        if (!cancelled) setLoadingRun(false);
      });
    return () => {
      cancelled = true;
    };
  }, [requestedRunId]);

  useEffect(() => {
    if (!runId) return;
    let cancelled = false;
    setInputsError(null);
    getRunInputs(runId)
      .then((data) => {
        if (!cancelled) setInputs(data);
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setInputs(null);
        setInputsError(toApiError(error).detail);
      });
    return () => {
      cancelled = true;
    };
  }, [runId]);

  useEffect(() => {
    if (!run) return;
    if (selectedVariantId && run.variants.some((variant) => variant.variant_id === selectedVariantId)) return;
    setSelectedVariantId(run.variants[0]?.variant_id);
  }, [run, selectedVariantId]);

  useEffect(() => {
    if (!runId || !selectedVariantId) {
      setVariantDetail(null);
      return;
    }
    let cancelled = false;
    getVariant(runId, selectedVariantId)
      .then((detail) => {
        if (!cancelled) setVariantDetail(detail);
      })
      .catch(() => {
        if (!cancelled) setVariantDetail(null);
      });
    return () => {
      cancelled = true;
    };
  }, [runId, selectedVariantId]);

  useEffect(() => {
    if (!runId || !selectedVariantId) {
      setExplanation(null);
      return;
    }
    let cancelled = false;
    setExplanationLoading(true);
    setExplanation(null);
    getVariantExplanation(runId, selectedVariantId)
      .then((data) => {
        if (!cancelled) setExplanation(data);
      })
      .catch(() => {
        if (!cancelled) setExplanation(null);
      })
      .finally(() => {
        if (!cancelled) setExplanationLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [runId, selectedVariantId]);

  useEffect(() => {
    let cancelled = false;
    listRoles()
      .then((roles) => {
        if (cancelled) return;
        setRoleNames(Object.fromEntries(roles.map((role) => [role.role_id, role.name_ru])));
      })
      .catch(() => {
        if (!cancelled) setRoleNames({});
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const start = async () => {
    setCreating(true);
    setPageError(null);
    setRecalcResult(null);
    setRecalcError(null);
    try {
      const created = await createRun(briefId);
      setRun(created);
      setSelectedVariantId(created.variants[0]?.variant_id);
      message.success('Расчёт завершён: варианты готовы к сравнению');
    } catch (error) {
      setPageError(toApiError(error));
    } finally {
      setCreating(false);
    }
  };

  const reload = async () => {
    if (!run) return;
    setLoadingRun(true);
    setPageError(null);
    try {
      const refreshed = await getRun(run.run_id);
      if (refreshed) setRun(refreshed);
    } catch (error) {
      setPageError(toApiError(error));
    } finally {
      setLoadingRun(false);
    }
  };

  const handleRecalculated = (response: RecalculateResponse, newRun: TeamRun) => {
    setRecalcResult(response);
    setRecalcError(null);
    setRun(newRun);
    setSelectedVariantId(newRun.variants[0]?.variant_id);
    setEditorOpen(false);
    if (response.reason_codes.includes('RECALC_APPLIED')) {
      message.success('Пересчёт применён: создан новый запуск');
    }
  };

  const handleRecalcError = (error: ApiError) => {
    setRecalcError(error);
    setRecalcResult(null);
  };

  const handleRecordDecision = async () => {
    if (!run || !selectedVariantId) return;
    setDeciding(true);
    setDecisionError(null);
    try {
      const resp = await recordDecision(run.run_id, {
        variant_id: selectedVariantId,
        decided_by: 'emp_manager1',
        comment: decisionComment.trim() || undefined,
      });
      setDecision(resp);
      setDecisionModalOpen(false);
      message.success('Решение утверждено и сохранено в аудите');
    } catch (err) {
      setDecisionError(toApiError(err));
    } finally {
      setDeciding(false);
    }
  };

  const parentLink = useMemo(() => {
    if (!run?.parent_run_id) return null;
    return `/runs/new/${run.brief_id}?run=${run.parent_run_id}`;
  }, [run]);

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Варианты команды</Title>

      <Space wrap>
        <Button type="primary" onClick={start} loading={creating}>
          Рассчитать варианты
        </Button>
        <Button onClick={reload} disabled={!run || loadingRun} loading={loadingRun}>
          Обновить
        </Button>
        <Button onClick={() => setEditorOpen(true)} disabled={!run}>
          Ограничения
        </Button>
        <Button
          type="primary"
          style={decision ? { background: '#059669', borderColor: '#059669' } : { background: '#10b981', borderColor: '#10b981' }}
          onClick={() => setDecisionModalOpen(true)}
          disabled={!run || !selectedVariantId || !!decision}
        >
          {decision ? 'Вариант утверждён' : 'Утвердить состав'}
        </Button>
        {selectedVariantId && <Text type="secondary">Базовый вариант: {selectedVariantId}</Text>}
      </Space>

      {decision && (
        <Alert
          type="success"
          showIcon
          data-testid="decision-success"
          message={`Состав команды утверждён (аудит ${decision.audit_event_id})`}
          description={
            <Space direction="vertical" size={0}>
              <Text>Утверждён вариант: <Text strong>{decision.variant_id}</Text></Text>
              <Text type="secondary">Дата решения: {new Date(decision.decided_at).toLocaleString('ru-RU')}</Text>
            </Space>
          }
        />
      )}

      {pageError && (
        <Alert
          type="error"
          showIcon
          message={`Ошибка: ${pageError.code}`}
          description={
            <Space direction="vertical" size={0}>
              <Text>{pageError.detail}</Text>
              {pageError.reasonCodes.length > 0 && (
                <Space wrap size={[4, 4]}>
                  {pageError.reasonCodes.map((code) => (
                    <Tag key={code} color="red">
                      {code}
                    </Tag>
                  ))}
                </Space>
              )}
              {pageError.requestId && (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  request_id: {pageError.requestId}
                </Text>
              )}
            </Space>
          }
        />
      )}

      {loadingRun && !run && <Spin tip="Загрузка запуска..." />}

      {recalcError && (
        <Alert
          type="error"
          showIcon
          message={`Пересчёт не выполнен: ${recalcError.code}`}
          description={
            <Space direction="vertical" size={0}>
              <Text>{recalcError.detail}</Text>
              {recalcError.reasonCodes.length > 0 && (
                <Space wrap size={[4, 4]}>
                  {recalcError.reasonCodes.map((code) => (
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

      {recalcResult && <RecalculationResult result={recalcResult} />}

      {!run && !loadingRun && !creating && (
        <Card>
          <AntEmpty description="Варианты ещё не рассчитаны. Нажмите «Рассчитать варианты»." />
        </Card>
      )}

      {run && (
        <Card
          size="small"
          title={
            <Space wrap>
              <Text strong>Запуск {run.run_id}</Text>
              <Tag color={STATUS_COLORS[run.status] ?? 'default'}>{run.status}</Tag>
              <Tag>{run.preset}</Tag>
              <Tag>{`${run.horizon.start_week}–${run.horizon.end_week}`}</Tag>
              {run.snapshot_id && <Tag>снимок {run.snapshot_id}</Tag>}
            </Space>
          }
          extra={
            parentLink ? (
              <Link to={parentLink}>{`родительский запуск ${run.parent_run_id}`}</Link>
            ) : (
              <Text type="secondary">первичный запуск</Text>
            )
          }
        >
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            {run.variant_set && (
              <Space wrap size={[8, 4]}>
                <Tag color={run.variant_set.diversity_rule_ok === false ? 'orange' : 'green'}>
                  {`разнообразие составов: ${
                    run.variant_set.min_pairwise_jaccard === undefined
                      ? '—'
                      : run.variant_set.min_pairwise_jaccard.toFixed(2)
                  }`}
                </Tag>
                {run.diagnostics && <Tag>{`solver ${run.diagnostics.solver_status}`}</Tag>}
                {run.diagnostics && <Tag>{`validator_rejected ${run.diagnostics.validator_rejected}`}</Tag>}
              </Space>
            )}

            {run.variants.length === 0 ? (
              <AntEmpty description="Варианты пока не сформированы" />
            ) : (
              <Radio.Group
                aria-label="Выбор варианта для пересчёта"
                value={selectedVariantId}
                onChange={(event) => setSelectedVariantId(event.target.value as string)}
                style={{ width: '100%' }}
              >
                <Row gutter={[12, 12]}>
                  {run.variants.map((variant) => (
                    <Col key={variant.variant_id} xs={24} lg={12}>
                      <VariantCard
                        variant={variant}
                        selected={variant.variant_id === selectedVariantId}
                        onSelect={setSelectedVariantId}
                      />
                    </Col>
                  ))}
                </Row>
              </Radio.Group>
            )}

            <Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
              Длительность всегда показана диапазоном P50/P80; балл — композитный Score с весами пресета «{run.preset}».
              Выбор варианта и добавленные ограничения уходят в журнал аудита.
            </Paragraph>
          </Space>
        </Card>
      )}

      {run && variantDetail && (
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <VariantDetailCard variant={variantDetail} roleNames={roleNames} />
          {explanationLoading ? (
            <ExplanationLoading />
          ) : explanation ? (
            <ExplanationPanel explanation={explanation} />
          ) : null}
        </Space>
      )}

      <ConstraintEditor
        open={editorOpen}
        run={run}
        inputs={inputs}
        inputsError={inputsError}
        baseVariantId={selectedVariantId}
        onClose={() => setEditorOpen(false)}
        onRecalculated={handleRecalculated}
        onError={handleRecalcError}
      />

      <Modal
        title={`Утвердить вариант команды: ${selectedVariantId}`}
        open={decisionModalOpen}
        onOk={handleRecordDecision}
        confirmLoading={deciding}
        onCancel={() => setDecisionModalOpen(false)}
        okText="Подтвердить решение"
        cancelText="Отмена"
      >
        <Space direction="vertical" size="middle" style={{ width: '100%', marginTop: 12 }}>
          <Text>
            Вы утверждаете вариант <Text strong>{selectedVariantId}</Text> в качестве финального состава проектной команды. Это действие фиксируется в журнале аудита.
          </Text>
          {decisionError && (
            <Alert type="error" showIcon message={decisionError.detail || decisionError.code} />
          )}
          <Input.TextArea
            rows={3}
            placeholder="Комментарий менеджера к решению (необязательно)"
            value={decisionComment}
            onChange={(e) => setDecisionComment(e.target.value)}
          />
        </Space>
      </Modal>
    </Space>
  );
}
